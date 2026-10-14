package tests

// Scenario tests of the STT job (TASK-029, PRD sections 7 and 8, AC-06, AC-08) on the real stack: SQLite, the file
// store, the whispercli adapter and the job runner. Only the clock and the whisper-cli binary are fake (ARCH-61): the
// test binary itself plays whisper-cli (main_test.go).

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/whispercli"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

// The adapters satisfy the ports of the transcribe use case.
var (
	_ transcribe.STTEngine            = (*whispercli.Engine)(nil)
	_ transcribe.TranscriptRepository = (*sqlite.TranscriptRepository)(nil)
	_ transcribe.RecordingReader      = (*sqlite.RecordingRepository)(nil)
	_ transcribe.ModelReader          = (*sqlite.ModelRepository)(nil)
	_ transcribe.ArtifactStore        = (*filestore.Store)(nil)
	_ jobrunner.Handler               = (*transcribe.Service)(nil)
)

const sttWorkDir = "stt-work"

var sttT0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// sttEvents records the published completion events.
type sttEvents struct {
	mu     sync.Mutex
	events []transcript.Created
}

func (e *sttEvents) TranscriptCreated(_ context.Context, ev transcript.Created) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
	return nil
}

func (e *sttEvents) list() []transcript.Created {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]transcript.Created(nil), e.events...)
}

type sttEnv struct {
	t           *testing.T
	dataRoot    string
	db          *sqlite.DB
	store       *filestore.Store
	jobs        *sqlite.JobRepository
	recordings  *sqlite.RecordingRepository
	models      *sqlite.ModelRepository
	transcripts *sqlite.TranscriptRepository
	clock       *testenv.FakeClock
	events      *sttEvents
	stt         *transcribe.Service
	runner      *jobrunner.Service
	rec         *recording.Recording
}

func newSTTEnv(t *testing.T) *sttEnv {
	t.Helper()
	dir := t.TempDir()
	e := &sttEnv{t: t, dataRoot: filepath.Join(dir, "data"), clock: testenv.NewFakeClock(sttT0), events: &sttEvents{}}

	store, err := filestore.New(e.dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	e.store = store
	e.db = openRunnerDB(t, filepath.Join(dir, "app.db"))
	e.jobs = sqlite.NewJobRepository(e.db)
	e.recordings = sqlite.NewRecordingRepository(e.db)
	e.models = sqlite.NewModelRepository(e.db)
	e.transcripts = sqlite.NewTranscriptRepository(e.db)

	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := whispercli.New(whispercli.Config{
		Binary: bin, DataRoot: e.dataRoot, WorkDir: filepath.Join(e.dataRoot, sttWorkDir), Threads: 1,
	}, store, store)
	if err != nil {
		t.Fatalf("whispercli.New: %v", err)
	}

	e.stt, err = transcribe.New(transcribe.Deps{
		Engine: engine, Transcripts: e.transcripts, Recordings: e.recordings, Models: e.models, Artifacts: store,
		Events: e.events, Clock: e.clock,
	})
	if err != nil {
		t.Fatal(err)
	}

	e.runner, err = jobrunner.New(jobrunner.Deps{Jobs: e.jobs, Clock: e.clock, Sleeper: e.clock}, runnerConfig(1))
	if err != nil {
		t.Fatal(err)
	}
	reg := jobrunner.Registration{Type: transcribe.JobType, Handler: e.stt, Heavy: true, Recovery: jobrunner.RecoveryRequeue}
	if err := e.runner.Register(reg); err != nil {
		t.Fatal(err)
	}

	e.rec = e.completedRecording("rec-1")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := e.runner.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
		if err := e.db.Close(); err != nil {
			t.Errorf("Close database: %v", err)
		}
		if err := e.store.Close(); err != nil {
			t.Errorf("Close store: %v", err)
		}
	})
	return e
}

// completedRecording stores a COMPLETED recording and its final track.
func (e *sttEnv) completedRecording(id shared.RecordingID) *recording.Recording {
	e.t.Helper()
	start := sttT0.Add(-2 * time.Hour)
	r, err := recording.New(recording.Spec{ID: id, MaxDuration: 3 * time.Hour}, start)
	if err != nil {
		e.t.Fatal(err)
	}
	end := start.Add(time.Hour)
	track, err := r.TrackRef("wav")
	if err != nil {
		e.t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { return r.Begin(start) }, func() error { return r.Stop(end) }, func() error { return r.Finalize(end) },
		func() error { return r.Complete(end, track, time.Hour) },
	} {
		if err := step(); err != nil {
			e.t.Fatal(err)
		}
	}
	if err := e.store.Write(context.Background(), track, strings.NewReader("RIFF fake audio")); err != nil {
		e.t.Fatal(err)
	}
	if err := e.recordings.Save(context.Background(), r); err != nil {
		e.t.Fatal(err)
	}
	return r
}

// installModel stores a model whose file content selects the fake whisper-cli scenario.
func (e *sttEnv) installModel(id shared.SttModelID, name, version, scenario string) *sttmodel.Model {
	e.t.Helper()
	file, err := shared.NewArtifactRef(shared.ArtifactModels, "ggml-"+string(id)+".bin")
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.store.Write(context.Background(), file, strings.NewReader(scenario)); err != nil {
		e.t.Fatal(err)
	}
	m, err := sttmodel.New(sttmodel.Spec{
		ID: id, Name: values.ModelName(name), ModelVersion: values.ModelVersion(version), File: file,
		SizeBytes: int64(len(scenario)), SHA256: values.SHA256(strings.Repeat("ab", 32)),
	}, sttT0.Add(-24*time.Hour))
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.models.Save(context.Background(), m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

// enqueue stores an STT job for the recording pinned to the model.
func (e *sttEnv) enqueue(id shared.JobID, rec shared.RecordingID, model shared.SttModelID) {
	e.t.Helper()
	req, err := values.NewSTTRequest(rec)
	if err != nil {
		e.t.Fatal(err)
	}
	j, err := job.New(job.Spec{
		ID: id, Type: transcribe.JobType, Key: fmt.Sprintf("stt:%s:%s", rec, model), Payload: req.Payload(),
		PinnedModelID: model, MaxAttempts: 2,
	}, e.clock.Now())
	if err != nil {
		e.t.Fatal(err)
	}
	if _, created, err := e.jobs.Enqueue(context.Background(), j); err != nil || !created {
		e.t.Fatalf("Enqueue: created=%v err=%v", created, err)
	}
}

func (e *sttEnv) job(id shared.JobID) *job.Job {
	e.t.Helper()
	j, err := e.jobs.Get(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return j
}

func (e *sttEnv) start() {
	e.t.Helper()
	if err := e.runner.Start(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (e *sttEnv) waitFor(id shared.JobID, s job.Status) {
	e.t.Helper()
	testenv.Eventually(e.t, fmt.Sprintf("job %s %s", id, s), func() bool { return e.job(id).Status() == s },
		func() { e.clock.Advance(time.Second) })
}

// runDirs returns the entries of the adapter's work directory: run directories left behind.
func (e *sttEnv) runDirs() []string {
	e.t.Helper()
	entries, err := os.ReadDir(filepath.Join(e.dataRoot, sttWorkDir))
	if err != nil && !os.IsNotExist(err) {
		e.t.Fatal(err)
	}
	var out []string
	for _, en := range entries {
		out = append(out, en.Name())
	}
	return out
}

func (e *sttEnv) transcriptFiles() []shared.ArtifactRef {
	e.t.Helper()
	refs, err := sqlite.NewRecordingTranscripts(e.db).ListArtifactsByRecording(context.Background(), e.rec.ID())
	if err != nil {
		e.t.Fatal(err)
	}
	return refs
}

// TestSTTJobTranscribesRecording is the test step "recording -> transcript saved, model/version recorded" (AC-06,
// AC-08): the job runner runs the STT handler, which runs whisper-cli (fake) with the pinned model and stores the
// transcript through the storage ports.
func TestSTTJobTranscribesRecording(t *testing.T) {
	e := newSTTEnv(t)
	e.installModel("m-default", "small", "1", testenv.WhisperFail) // another installed model: the pin decides
	pinned := e.installModel("m-pinned", "base", "2024-03", testenv.WhisperOK)
	e.enqueue("job-1", e.rec.ID(), pinned.ID())
	e.start()
	e.waitFor("job-1", job.StatusCompleted)

	if j := e.job("job-1"); j.Progress().Percent() != 100 || j.LastError() != "" || j.Attempts() != 1 {
		t.Errorf("job: percent %d, last error %q, attempts %d", j.Progress().Percent(), j.LastError(), j.Attempts())
	}

	tr, err := e.transcripts.Get(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("transcript not stored: %v", err)
	}
	if tr.RecordingID() != e.rec.ID() || tr.ModelID() != "m-pinned" || tr.ModelName() != "base" ||
		tr.ModelVersion() != "2024-03" || tr.Language() != "ru" {
		t.Fatalf("transcript: recording %s, model %s %s %s, language %q", tr.RecordingID(), tr.ModelID(), tr.ModelName(),
			tr.ModelVersion(), tr.Language())
	}

	// The document is the domain format, written by the adapter below transcripts/.
	rc, err := e.store.Open(context.Background(), tr.Text())
	if err != nil {
		t.Fatalf("open document: %v", err)
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := transcript.ParseDocument(data)
	if err != nil {
		t.Fatalf("stored document: %v", err)
	}
	var texts []string
	for _, s := range doc.Segments() {
		texts = append(texts, s.Text)
	}
	if !reflect.DeepEqual(texts, testenv.WhisperOKSegments) {
		t.Errorf("segments = %q, want %q", texts, testenv.WhisperOKSegments)
	}

	want := transcript.Created{Transcript: "job-1", Recording: e.rec.ID(), ModelName: "base", ModelVersion: "2024-03", Language: "ru"}
	if ev := e.events.list(); len(ev) != 1 || ev[0] != want {
		t.Errorf("events = %+v, want [%+v]", ev, want)
	}
	if dirs := e.runDirs(); len(dirs) != 0 {
		t.Errorf("run directories left: %v", dirs)
	}

	// A rerun of the same job (a crash before COMPLETED was saved) stores nothing new and publishes the event again.
	if err := e.stt.Handle(context.Background(), e.job("job-1").Snapshot(), nil); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if files := e.transcriptFiles(); len(files) != 1 {
		t.Errorf("transcripts after the rerun: %v", files)
	}
	if ev := e.events.list(); len(ev) != 2 || ev[1] != want {
		t.Errorf("events after the rerun = %+v", ev)
	}

	// AC-08: the model name and version stay readable after the model is removed.
	if err := e.models.Delete(context.Background(), "m-pinned"); err != nil {
		t.Fatalf("remove the model: %v", err)
	}
	tr, err = e.transcripts.Get(context.Background(), "job-1")
	if err != nil || tr.ModelID() != "" || tr.ModelName() != "base" || tr.ModelVersion() != "2024-03" {
		t.Fatalf("after removal: %v, model %q %q %q", err, tr.ModelID(), tr.ModelName(), tr.ModelVersion())
	}
}

// TestSTTJobCancelKillsWhisper: cancelling a running STT job kills the sidecar and leaves no transcript and no files.
func TestSTTJobCancelKillsWhisper(t *testing.T) {
	e := newSTTEnv(t)
	m := e.installModel("m-slow", "base", "1", testenv.WhisperHang)
	e.enqueue("job-1", e.rec.ID(), m.ID())
	e.start()

	// The fake prints 10% and then hangs: the report proves the process runs.
	testenv.Eventually(t, "the sidecar reports progress", func() bool {
		j := e.job("job-1")
		return j.Status() == job.StatusRunning && j.Progress().Percent() > 0
	}, func() { e.clock.Advance(time.Second) })

	if err := e.runner.Cancel(context.Background(), "job-1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	e.waitFor("job-1", job.StatusCancelled)

	if _, err := e.transcripts.Get(context.Background(), "job-1"); err == nil {
		t.Error("a cancelled job stored a transcript")
	}
	if dirs := e.runDirs(); len(dirs) != 0 {
		t.Errorf("run directories left: %v", dirs)
	}
	ref, err := transcript.DocumentRef("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.Open(context.Background(), ref); err == nil {
		t.Error("a cancelled job left a document")
	}
	if ev := e.events.list(); len(ev) != 0 {
		t.Errorf("events = %v", ev)
	}
}

// TestSTTJobFailureIsBoundedAndSanitised: a failing sidecar is retried up to the cap, then the job fails with the
// fixed message only (no paths or stderr in last_error).
func TestSTTJobFailureIsBoundedAndSanitised(t *testing.T) {
	e := newSTTEnv(t)
	m := e.installModel("m-broken", "base", "1", testenv.WhisperFail)
	e.enqueue("job-1", e.rec.ID(), m.ID())
	e.start()
	e.waitFor("job-1", job.StatusFailed)

	j := e.job("job-1")
	if j.Attempts() != 2 || !strings.HasSuffix(j.LastError(), transcribe.ErrTranscriptionFailed.Error()) {
		t.Errorf("attempts %d, last error %q", j.Attempts(), j.LastError())
	}
	for _, leak := range []string{e.dataRoot, "stderr", "bad magic", "models/"} {
		if strings.Contains(j.LastError(), leak) {
			t.Errorf("last error %q contains %q", j.LastError(), leak)
		}
	}
}

// TestSTTJobMissingRecordingFailsAtOnce: an STT job for a recording that is gone fails on its first attempt.
func TestSTTJobMissingRecordingFailsAtOnce(t *testing.T) {
	e := newSTTEnv(t)
	m := e.installModel("m-1", "base", "1", testenv.WhisperOK)
	e.enqueue("job-1", "rec-gone", m.ID())
	e.start()
	e.waitFor("job-1", job.StatusFailed)

	if j := e.job("job-1"); j.Attempts() != 1 || !strings.Contains(j.LastError(), transcribe.ErrRecordingNotFound.Error()) {
		t.Errorf("attempts %d, last error %q", j.Attempts(), j.LastError())
	}
}
