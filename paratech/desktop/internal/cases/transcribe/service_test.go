package transcribe

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/jobs/services/powerpolicy"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/testenv"
)

func TestNewRequiresPorts(t *testing.T) {
	e := newEnv(t)
	full := Deps{
		Engine: e.engine, Transcripts: e.transcripts, Recordings: e.recordings, Models: e.models, Artifacts: e.artifacts,
		Events: e.events, Clock: e.clock,
	}
	drop := map[string]func(*Deps){
		"engine":      func(d *Deps) { d.Engine = nil },
		"transcripts": func(d *Deps) { d.Transcripts = nil },
		"recordings":  func(d *Deps) { d.Recordings = nil },
		"models":      func(d *Deps) { d.Models = nil },
		"artifacts":   func(d *Deps) { d.Artifacts = nil },
		"events":      func(d *Deps) { d.Events = nil },
		"clock":       func(d *Deps) { d.Clock = nil },
	}
	for name, change := range drop {
		t.Run(name, func(t *testing.T) {
			d := full
			change(&d)
			if _, err := New(d); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("New without %s = %v, want ErrInvalidConfig", name, err)
			}
		})
	}
	if _, err := New(full); err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestHandleSuccess(t *testing.T) {
	e := newEnv(t)
	e.clock.Advance(time.Minute)
	rep := &reporter{}

	if err := e.svc.Handle(context.Background(), e.defaultJob(t), rep.report); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// The engine got the recording's final track, the pinned model's file and the job's document reference.
	calls := e.engine.Calls()
	want := testenv.STTCall{Audio: e.rec.Audio(), Model: e.model.File(), Output: docRef(t)}
	if len(calls) != 1 || calls[0] != want {
		t.Fatalf("engine calls = %+v, want [%+v]", calls, want)
	}

	// AC-08: the transcript names the pinned model by id, name and version.
	tr, err := e.transcripts.Get(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("transcript not stored: %v", err)
	}
	if tr.RecordingID() != "rec-1" || tr.ModelID() != "m-1" || tr.ModelName() != "base" || tr.ModelVersion() != "1" ||
		tr.Language() != "ru" || tr.Text() != docRef(t) || !tr.CreatedAt().Equal(t0.Add(time.Minute)) {
		t.Fatalf("transcript = %+v", stateOf(tr, tr.Version()))
	}

	if got := rep.percents(); !reflect.DeepEqual(got, []int{0, 45, 90, documentStored}) {
		t.Errorf("reported percents = %v", got)
	}
	if got := rep.checkpoints(); !reflect.DeepEqual(got, []string{CheckpointTranscribed}) {
		t.Errorf("checkpoints = %v", got)
	}

	wantEvent := transcript.Created{Transcript: "job-1", Recording: "rec-1", ModelName: "base", ModelVersion: "1", Language: "ru"}
	if got := e.events.list(); len(got) != 1 || got[0] != wantEvent {
		t.Errorf("events = %+v, want [%+v]", got, wantEvent)
	}
}

func TestProgressForwarding(t *testing.T) {
	tests := []struct {
		name   string
		engine []int
		want   []int
	}{
		{name: "scaled to the engine share", engine: []int{0, 50, 100}, want: []int{0, 45, 90, documentStored}},
		{name: "repeats and steps that do not move the scaled value are dropped", engine: []int{0, 1, 2, 50, 50, 100},
			want: []int{0, 1, 45, 90, documentStored}},
		{name: "out of range values are clamped", engine: []int{-5, 150}, want: []int{0, 90, documentStored}},
		{name: "no engine progress", engine: nil, want: []int{documentStored}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.engine.SetProgress(tc.engine...)
			rep := &reporter{}
			if err := e.svc.Handle(context.Background(), e.defaultJob(t), rep.report); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if got := rep.percents(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("percents = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCancellationStopsTheEngine(t *testing.T) {
	e := newEnv(t)
	e.engine.Block(true)
	ctx, cancel := context.WithCancel(context.Background())
	rep := &reporter{}

	done := make(chan error, 1)
	go func() { done <- e.svc.Handle(ctx, e.defaultJob(t), rep.report) }()
	testenv.Eventually(t, "the engine runs", func() bool { return len(e.engine.Calls()) == 1 && len(rep.percents()) > 0 }, nil)
	cancel()

	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Handle did not return after cancellation")
	}
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrTranscriptionFailed) {
		t.Fatalf("Handle = %v, want ErrTranscriptionFailed wrapping context.Canceled", err)
	}
	if backoff.Classify(err) == backoff.ClassPermanent {
		t.Error("a cancellation must not be a permanent failure")
	}
	assertNothingStored(t, e)
}

func assertNothingStored(t *testing.T, e *env) {
	t.Helper()
	if n := e.transcripts.count(); n != 0 {
		t.Errorf("%d transcripts stored", n)
	}
	if p := e.artifacts.Paths(); len(p) != 0 {
		t.Errorf("artifacts written: %v", p)
	}
	if ev := e.events.list(); len(ev) != 0 {
		t.Errorf("events published: %v", ev)
	}
}

func TestInvalidInputsFailPermanently(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, e *env) job.Snapshot
		want    error
	}{
		{
			name:    "malformed payload",
			prepare: func(t *testing.T, e *env) job.Snapshot { return sttJob(t, "job-1", `{"recording":"rec-1"}`, "m-1") },
			want:    ErrInvalidPayload,
		},
		{
			name:    "no pinned model",
			prepare: func(t *testing.T, e *env) job.Snapshot { return sttJob(t, "job-1", payloadFor(t, "rec-1"), "") },
			want:    ErrNoPinnedModel,
		},
		{
			name: "recording deleted",
			prepare: func(t *testing.T, e *env) job.Snapshot {
				delete(e.recordings.rows, "rec-1")
				return e.defaultJob(t)
			},
			want: ErrRecordingNotFound,
		},
		{
			name: "recording not completed",
			prepare: func(t *testing.T, e *env) job.Snapshot {
				e.recordings.rows["rec-1"] = startedRecording(t, "rec-1")
				return e.defaultJob(t)
			},
			want: ErrRecordingNotReady,
		},
		{
			name: "pinned model removed",
			prepare: func(t *testing.T, e *env) job.Snapshot {
				delete(e.models.rows, "m-1")
				return e.defaultJob(t)
			},
			want: ErrModelNotInstalled,
		},
		{
			name: "audio or model file missing",
			prepare: func(t *testing.T, e *env) job.Snapshot {
				e.rebuild(t, engineFunc(func(context.Context, shared.ArtifactRef, shared.ArtifactRef, shared.ArtifactRef, func(int)) error {
					return errors.Join(errors.New(`C:\Users\student\secret.wav`), shared.ErrNotFound)
				}))
				return e.defaultJob(t)
			},
			want: ErrInputMissing,
		},
		{
			name: "engine wrote an invalid document",
			prepare: func(t *testing.T, e *env) job.Snapshot {
				e.rebuild(t, engineFunc(func(ctx context.Context, _, _, out shared.ArtifactRef, _ func(int)) error {
					return e.artifacts.Write(ctx, out, strings.NewReader("{not a document"))
				}))
				return e.defaultJob(t)
			},
			want: ErrInvalidDocument,
		},
		{
			name: "engine reported success without a document",
			prepare: func(t *testing.T, e *env) job.Snapshot {
				e.rebuild(t, engineFunc(func(context.Context, shared.ArtifactRef, shared.ArtifactRef, shared.ArtifactRef, func(int)) error {
					return nil
				}))
				return e.defaultJob(t)
			},
			want: ErrInvalidDocument,
		},
		{
			name: "stored transcript of another recording",
			prepare: func(t *testing.T, e *env) job.Snapshot {
				other, err := transcript.New(transcript.Spec{
					ID: "job-1", RecordingID: "rec-other", ModelID: "m-1", ModelName: "base", ModelVersion: "1", Text: docRef(t),
				}, t0)
				if err != nil {
					t.Fatal(err)
				}
				if err := e.transcripts.Save(context.Background(), other); err != nil {
					t.Fatal(err)
				}
				return e.defaultJob(t)
			},
			want: ErrTranscriptConflict,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			snap := tc.prepare(t, e)
			err := e.svc.Handle(context.Background(), snap, (&reporter{}).report)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Handle = %v, want %v", err, tc.want)
			}
			if backoff.Classify(err) != backoff.ClassPermanent {
				t.Errorf("error %v is not permanent", err)
			}
			// last_error is the message: fixed text, no paths or ids from the cause.
			if msg := err.Error(); strings.Contains(msg, "secret") || strings.Contains(msg, "rec-") || strings.Contains(msg, `\`) {
				t.Errorf("error text leaks details: %q", msg)
			}
			if ev := e.events.list(); len(ev) != 0 {
				t.Errorf("events published on failure: %v", ev)
			}
		})
	}
}

func TestTransientFailures(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, e *env)
		want    error
		class   backoff.Class
	}{
		{
			name:    "engine failure is retried with a fixed message",
			prepare: func(t *testing.T, e *env) { e.engine.FailWith(errors.New(`exit code 10: stderr: <data>\secret`)) },
			want:    ErrTranscriptionFailed, class: backoff.ClassTransient,
		},
		{
			name:    "permission denied fails at once",
			prepare: func(t *testing.T, e *env) { e.engine.FailWith(shared.ErrStoragePermission) },
			want:    ErrTranscriptionFailed, class: backoff.ClassPermanent,
		},
		{
			name:    "recording store unavailable",
			prepare: func(t *testing.T, e *env) { e.recordings.err = errors.New("database is locked") },
			want:    ErrStorage, class: backoff.ClassTransient,
		},
		{
			name:    "model store unavailable",
			prepare: func(t *testing.T, e *env) { e.models.err = errors.New("database is locked") },
			want:    ErrStorage, class: backoff.ClassTransient,
		},
		{
			name:    "transcript store unavailable",
			prepare: func(t *testing.T, e *env) { e.transcripts.getErr = errors.New("database is locked") },
			want:    ErrStorage, class: backoff.ClassTransient,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			tc.prepare(t, e)
			err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Handle = %v, want %v", err, tc.want)
			}
			if got := backoff.Classify(err); got != tc.class {
				t.Errorf("class = %v, want %v", got, tc.class)
			}
			if err.Error() != tc.want.Error() {
				t.Errorf("message = %q, want the fixed %q", err.Error(), tc.want.Error())
			}
			if n := e.transcripts.count(); n != 0 {
				t.Errorf("%d transcripts stored", n)
			}
		})
	}
}

// TestRerunIsIdempotent: a second attempt of the same job (crash before the job completed, or a retry) creates no
// second transcript, runs no second recognition and publishes the same event again.
func TestRerunIsIdempotent(t *testing.T) {
	e := newEnv(t)
	snap := e.defaultJob(t)
	for range 2 {
		if err := e.svc.Handle(context.Background(), snap, (&reporter{}).report); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}
	if n := len(e.engine.Calls()); n != 1 {
		t.Errorf("engine ran %d times, want 1", n)
	}
	if e.transcripts.count() != 1 || e.transcripts.saves != 1 {
		t.Errorf("transcripts = %d after %d saves, want 1 after 1", e.transcripts.count(), e.transcripts.saves)
	}
	ev := e.events.list()
	if len(ev) != 2 || ev[0] != ev[1] || ev[0].Transcript != "job-1" {
		t.Errorf("events = %+v, want the same event twice", ev)
	}
	if p := e.artifacts.Paths(); len(p) != 1 || p[0] != docRef(t).String() {
		t.Errorf("artifacts = %v, want only %s", p, docRef(t))
	}
}

// TestResumeAfterDocumentStored: a crash between the engine's write and the transcript row skips the engine.
func TestResumeAfterDocumentStored(t *testing.T) {
	e := newEnv(t)
	data, err := e.doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.artifacts.Write(context.Background(), docRef(t), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	rep := &reporter{}
	if err := e.svc.Handle(context.Background(), e.defaultJob(t), rep.report); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if n := len(e.engine.Calls()); n != 0 {
		t.Errorf("engine ran %d times, want 0", n)
	}
	if got := rep.percents(); !reflect.DeepEqual(got, []int{documentStored}) {
		t.Errorf("percents = %v", got)
	}
	if e.transcripts.count() != 1 || len(e.events.list()) != 1 {
		t.Errorf("transcripts %d, events %d; want 1 and 1", e.transcripts.count(), len(e.events.list()))
	}
}

// TestInvalidStoredDocumentIsRedone: a document that does not parse is not trusted; the engine runs again.
func TestInvalidStoredDocumentIsRedone(t *testing.T) {
	e := newEnv(t)
	if err := e.artifacts.Write(context.Background(), docRef(t), strings.NewReader(`{"version": 99}`)); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if n := len(e.engine.Calls()); n != 1 {
		t.Errorf("engine ran %d times, want 1", n)
	}
	tr, err := e.transcripts.Get(context.Background(), "job-1")
	if err != nil || tr.Language() != "ru" {
		t.Fatalf("transcript = %v, %v", tr, err)
	}
}

// TestFollowUpEventIsRetriedIdempotently: a failed publish fails the attempt as transient; the retry publishes
// without a second recognition or transcript.
func TestFollowUpEventIsRetriedIdempotently(t *testing.T) {
	e := newEnv(t)
	e.events.fail = errors.New("queue busy")
	snap := e.defaultJob(t)

	err := e.svc.Handle(context.Background(), snap, (&reporter{}).report)
	if !errors.Is(err, ErrPublish) || backoff.Classify(err) != backoff.ClassTransient {
		t.Fatalf("Handle = %v, want a transient ErrPublish", err)
	}
	if e.transcripts.count() != 1 {
		t.Fatalf("the transcript must be stored before the event: %d", e.transcripts.count())
	}

	e.events.fail = nil
	if err := e.svc.Handle(context.Background(), snap, (&reporter{}).report); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(e.engine.Calls()) != 1 || e.transcripts.saves != 1 || len(e.events.list()) != 1 {
		t.Errorf("engine %d, saves %d, events %d; want 1, 1, 1", len(e.engine.Calls()), e.transcripts.saves, len(e.events.list()))
	}
}

// TestConcurrentInsertUsesStoredTranscript: an insert that meets an existing row loads it instead of failing.
func TestConcurrentInsertUsesStoredTranscript(t *testing.T) {
	e := newEnv(t)
	e.transcripts.beforeSave = func(f *fakeTranscripts) {
		f.beforeSave = nil
		other, err := transcript.New(transcript.Spec{
			ID: "job-1", RecordingID: "rec-1", ModelID: "m-1", ModelName: "base", ModelVersion: "1", Language: "en", Text: docRef(t),
		}, t0)
		if err != nil {
			t.Error(err)
			return
		}
		f.rows[other.ID()] = stateOf(other, 1)
	}
	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if ev := e.events.list(); len(ev) != 1 || ev[0].Language != "en" {
		t.Fatalf("events = %+v, want the stored transcript's event", ev)
	}
}

// TestPowerLossStopsAtTheNextReport is PRD section 14 case 12: the runner's report returns powerpolicy.ErrPowerLost,
// the engine process is stopped, nothing is stored, and the error reaches the runner so the job waits for power.
func TestPowerLossStopsAtTheNextReport(t *testing.T) {
	e := newEnv(t)
	e.engine.Block(true) // without the stop the engine would run until the test times out
	rep := &reporter{failAt: 2, err: powerpolicy.ErrPowerLost}
	snap := e.defaultJob(t)

	done := make(chan error, 1)
	go func() { done <- e.svc.Handle(context.Background(), snap, rep.report) }()
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Handle did not stop after the power loss")
	}
	if !errors.Is(err, powerpolicy.ErrPowerLost) || !errors.Is(err, ErrStopped) {
		t.Fatalf("Handle = %v, want ErrStopped wrapping powerpolicy.ErrPowerLost", err)
	}
	if backoff.Classify(err) == backoff.ClassPermanent {
		t.Error("a power loss must not fail the job")
	}
	assertNothingStored(t, e)

	// AC power is back: the next attempt runs the engine again from the start (whisper-cli cannot resume inside a file).
	e.engine.Block(false)
	if err := e.svc.Handle(context.Background(), snap, (&reporter{}).report); err != nil {
		t.Fatalf("resumed attempt: %v", err)
	}
	if e.transcripts.count() != 1 || len(e.engine.Calls()) != 2 {
		t.Errorf("transcripts %d, engine calls %d; want 1 and 2", e.transcripts.count(), len(e.engine.Calls()))
	}
}

// TestPowerLossAtTheCheckpoint: the power is lost right when the document is stored; the next attempt resumes from
// the document without running the engine.
func TestPowerLossAtTheCheckpoint(t *testing.T) {
	e := newEnv(t)
	e.engine.SetProgress() // no engine reports: the checkpoint is the first report
	rep := &reporter{failAt: 1, err: powerpolicy.ErrPowerLost}
	snap := e.defaultJob(t)

	if err := e.svc.Handle(context.Background(), snap, rep.report); !errors.Is(err, powerpolicy.ErrPowerLost) {
		t.Fatalf("Handle = %v, want powerpolicy.ErrPowerLost", err)
	}
	if e.transcripts.count() != 0 {
		t.Fatal("no transcript before the checkpoint was accepted")
	}

	if err := e.svc.Handle(context.Background(), snap, (&reporter{}).report); err != nil {
		t.Fatalf("resumed attempt: %v", err)
	}
	if len(e.engine.Calls()) != 1 || e.transcripts.count() != 1 {
		t.Errorf("engine calls %d, transcripts %d; want 1 and 1", len(e.engine.Calls()), e.transcripts.count())
	}
}

// TestChangedElsewhereStops: a report conflict (the job was cancelled or changed elsewhere) stops the attempt too.
func TestChangedElsewhereStops(t *testing.T) {
	e := newEnv(t)
	e.engine.Block(true)
	rep := &reporter{failAt: 1, err: shared.ErrVersionConflict}
	err := e.svc.Handle(context.Background(), e.defaultJob(t), rep.report)
	if !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Handle = %v, want a conflict", err)
	}
	assertNothingStored(t, e)
}
