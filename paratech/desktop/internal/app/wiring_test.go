package app

// The wired core on the real stack (SQLite, the file store, whispercli with the fake binary, the job runner, the
// recording session); only the recorder, the power source and whisper-cli are fake (ARCH-61). The actions go through
// the bindings of the recordings and jobs page.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	tvalues "paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// pageBindings is the binding of the recordings and jobs page on the opened core.
func pageBindings(core wails.Core) *wails.ActivityService {
	return wails.NewActivityService(core.Activity, core.Stopper, core.Forcer, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// The Stop button ends a running recording: the stored recording is COMPLETED with the chunk it captured.
func TestStopButtonStopsTheRecording(t *testing.T) {
	ctx := context.Background()
	a, f := newTestAppWith(t, t.TempDir())
	core := startCore(t, a)

	// The background recovery refuses a start while it marks interrupted recordings; it is short.
	var info recordingsession.Info
	testenv.Eventually(t, "a recording starts", func() bool {
		var err error
		info, err = a.core.recordings.Start(ctx, recordingsession.StartRequest{})
		if err != nil && !errors.Is(err, recordingsession.ErrRecovering) {
			t.Fatal(err)
		}
		return err == nil
	}, nil)
	if _, err := f.recorder.Feed(ctx, info.ID, []byte("audio")); err != nil {
		t.Fatal(err)
	}

	if err := pageBindings(core).StopRecording(ctx, string(info.ID)); err != nil {
		t.Fatalf("StopRecording: %v", err)
	}
	rec, err := sqlite.NewRecordingRepository(a.core.db).Get(ctx, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status() != recording.StatusCompleted || rec.Audio().IsZero() {
		t.Fatalf("recording %s, audio %q; want COMPLETED with a track", rec.Status(), rec.Audio())
	}
	if f.recorder.Running(info.ID) {
		t.Fatal("the capture still runs")
	}
}

// Run now starts an STT job that waits for AC power; the transcript it stores enqueues the post-processing jobs,
// which wait for the network while the Hub is not configured, without using attempts.
func TestRunNowTranscribesAndEnqueuesPostProcessing(t *testing.T) {
	ctx := context.Background()
	a, f := newTestAppWith(t, t.TempDir())
	f.power.Set(false)      // on battery with the default "only on AC power"
	core := startCore(t, a) // first: the startup cleanup of temporary files runs before any write
	jobs := sqlite.NewJobRepository(a.core.db)
	model := installModel(t, a, "m1", testenv.WhisperOK)
	rec := completedRecording(t, a, "rec-1")
	enqueueSTT(t, a, "job-stt", rec, model)

	status := func(id shared.JobID) job.Status {
		j, err := jobs.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return j.Status()
	}
	testenv.Eventually(t, "STT job waits for AC power", func() bool { return status("job-stt") == job.StatusWaitingForPower }, nil)

	if err := pageBindings(core).RunJobNow(ctx, "job-stt"); err != nil {
		t.Fatalf("RunJobNow: %v", err)
	}
	testenv.Eventually(t, "STT job completes", func() bool { return status("job-stt") == job.StatusCompleted }, nil)

	keys := map[string]job.Type{
		fmt.Sprintf("%s:job-stt:v%d", homeworkflow.SummarizeJobType, prompts.Version):       homeworkflow.SummarizeJobType,
		fmt.Sprintf("%s:job-stt:v%d", homeworkflow.ExtractHomeworkJobType, prompts.Version): homeworkflow.ExtractHomeworkJobType,
	}
	waiting := func() []*job.Job {
		list, err := jobs.ListByStatus(ctx, job.StatusWaitingForNetwork)
		if err != nil {
			t.Fatal(err)
		}
		return list
	}
	testenv.Eventually(t, "post-processing jobs wait for the network", func() bool { return len(waiting()) == len(keys) }, nil)
	for _, j := range waiting() {
		if typ, ok := keys[j.Key()]; !ok || typ != j.Type() || j.Attempts() != 0 {
			t.Errorf("job %s %s key %q attempts %d; want one per key %v and no attempt used", j.ID(), j.Type(), j.Key(),
				j.Attempts(), keys)
		}
	}
	all, _, err := jobs.ListActivity(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("%d jobs, want the STT job and one job per post-processing type", len(all))
	}
}

// installModel stores an installed model whose file content selects the fake whisper-cli scenario.
func installModel(t *testing.T, a *App, id shared.SttModelID, scenario string) shared.SttModelID {
	t.Helper()
	file, err := shared.NewArtifactRef(shared.ArtifactModels, "ggml-"+string(id)+".bin")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.core.store.Write(context.Background(), file, strings.NewReader(scenario)); err != nil {
		t.Fatal(err)
	}
	m, err := sttmodel.New(sttmodel.Spec{
		ID: id, Name: tvalues.ModelName("base"), ModelVersion: tvalues.ModelVersion("1"), File: file,
		SizeBytes: int64(len(scenario)), SHA256: tvalues.SHA256(strings.Repeat("ab", 32)),
	}, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewModelRepository(a.core.db).Save(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	return id
}

// completedRecording stores a COMPLETED recording with its final track.
func completedRecording(t *testing.T, a *App, id shared.RecordingID) shared.RecordingID {
	t.Helper()
	start := time.Now().Add(-2 * time.Hour)
	r, err := recording.New(recording.Spec{ID: id, MaxDuration: 3 * time.Hour}, start)
	if err != nil {
		t.Fatal(err)
	}
	end := start.Add(time.Hour)
	track, err := r.TrackRef("wav")
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { return r.Begin(start) }, func() error { return r.Stop(end) }, func() error { return r.Finalize(end) },
		func() error { return r.Complete(end, track, time.Hour) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.core.store.Write(context.Background(), track, strings.NewReader("RIFF fake audio")); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewRecordingRepository(a.core.db).Save(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	return id
}

// enqueueSTT stores an STT job for the recording pinned to the model.
func enqueueSTT(t *testing.T, a *App, id shared.JobID, rec shared.RecordingID, model shared.SttModelID) {
	t.Helper()
	req, err := tvalues.NewSTTRequest(rec)
	if err != nil {
		t.Fatal(err)
	}
	j, err := job.New(job.Spec{
		ID: id, Type: transcribe.JobType, Key: fmt.Sprintf("stt:%s:%s", rec, model), Payload: req.Payload(),
		PinnedModelID: model, MaxAttempts: 2,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := sqlite.NewJobRepository(a.core.db).Enqueue(context.Background(), j); err != nil || !created {
		t.Fatalf("Enqueue: created=%v err=%v", created, err)
	}
}
