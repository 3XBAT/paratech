package transcribe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

var t0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// handler is the Handler port of the job runner (use cases do not import each other, ARCH-20).
type handler interface {
	Handle(ctx context.Context, snap job.Snapshot, report func(ctx context.Context, p job.Progress) error) error
}

var _ handler = (*Service)(nil)

// fakeTranscripts keeps stored copies, like a database, with the version protocol of ADR-0004(d).
type fakeTranscripts struct {
	mu     sync.Mutex
	rows   map[shared.TranscriptID]transcript.State
	getErr error
	// beforeSave runs inside Save before the write, for example to simulate a concurrent insert.
	beforeSave func(f *fakeTranscripts)
	saves      int
}

func newFakeTranscripts() *fakeTranscripts {
	return &fakeTranscripts{rows: map[shared.TranscriptID]transcript.State{}}
}

func stateOf(t *transcript.Transcript, version int64) transcript.State {
	return transcript.State{
		Spec: transcript.Spec{
			ID: t.ID(), RecordingID: t.RecordingID(), ModelID: t.ModelID(), ModelName: t.ModelName(),
			ModelVersion: t.ModelVersion(), Language: t.Language(), Text: t.Text(),
		},
		Version: version, CreatedAt: t.CreatedAt(),
	}
}

func (f *fakeTranscripts) Get(ctx context.Context, id shared.TranscriptID) (*transcript.Transcript, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	s, ok := f.rows[id]
	if !ok {
		return nil, fmt.Errorf("%w: transcript %s", shared.ErrNotFound, id)
	}
	return transcript.Restore(s)
}

func (f *fakeTranscripts) Save(ctx context.Context, t *transcript.Transcript) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beforeSave != nil {
		f.beforeSave(f)
	}
	cur, exists := f.rows[t.ID()]
	switch {
	case t.Version() == 0 && exists:
		return shared.ErrVersionConflict
	case t.Version() > 0 && (!exists || cur.Version != t.Version()):
		return shared.ErrVersionConflict
	}
	next := t.Version() + 1
	f.rows[t.ID()] = stateOf(t, next)
	f.saves++
	t.Persisted(next)
	return nil
}

func (f *fakeTranscripts) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

type fakeRecordings struct {
	rows map[shared.RecordingID]*recording.Recording
	err  error
}

func (f *fakeRecordings) Get(_ context.Context, id shared.RecordingID) (*recording.Recording, error) {
	if f.err != nil {
		return nil, f.err
	}
	r, ok := f.rows[id]
	if !ok {
		return nil, fmt.Errorf("%w: recording %s", shared.ErrNotFound, id)
	}
	return r, nil
}

type fakeModels struct {
	rows map[shared.SttModelID]*sttmodel.Model
	err  error
}

func (f *fakeModels) Get(_ context.Context, id shared.SttModelID) (*sttmodel.Model, error) {
	if f.err != nil {
		return nil, f.err
	}
	m, ok := f.rows[id]
	if !ok {
		return nil, fmt.Errorf("%w: model %s", shared.ErrNotFound, id)
	}
	return m, nil
}

// fakeEvents records published events; fail makes the next calls fail.
type fakeEvents struct {
	mu     sync.Mutex
	events []transcript.Created
	fail   error
}

func (f *fakeEvents) TranscriptCreated(_ context.Context, e transcript.Created) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.events = append(f.events, e)
	return nil
}

func (f *fakeEvents) list() []transcript.Created {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]transcript.Created(nil), f.events...)
}

// reporter records progress reports; failAt makes the n-th report (1-based) and every later one return err.
type reporter struct {
	mu      sync.Mutex
	reports []job.Progress
	failAt  int
	err     error
}

func (r *reporter) report(ctx context.Context, p job.Progress) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failAt > 0 && len(r.reports)+1 >= r.failAt {
		return r.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.reports = append(r.reports, p)
	return nil
}

func (r *reporter) percents() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int, len(r.reports))
	for i, p := range r.reports {
		out[i] = p.Percent()
	}
	return out
}

func (r *reporter) checkpoints() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, p := range r.reports {
		if p.Checkpoint() != "" {
			out = append(out, p.Checkpoint())
		}
	}
	return out
}

// engineFunc is an STTEngine from a function, for behaviour FakeSTT does not have.
type engineFunc func(ctx context.Context, audio, model, output shared.ArtifactRef, progress func(int)) error

func (f engineFunc) Transcribe(ctx context.Context, audio, model, output shared.ArtifactRef, progress func(int)) error {
	return f(ctx, audio, model, output, progress)
}

// env is a Service on fakes with one completed recording "rec-1" and one installed model "m-1".
type env struct {
	svc         *Service
	engine      *testenv.FakeSTT
	artifacts   *testenv.MemArtifacts
	transcripts *fakeTranscripts
	recordings  *fakeRecordings
	models      *fakeModels
	events      *fakeEvents
	clock       *testenv.FakeClock
	rec         *recording.Recording
	model       *sttmodel.Model
	doc         transcript.Document
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		artifacts: testenv.NewMemArtifacts(), transcripts: newFakeTranscripts(), events: &fakeEvents{},
		clock: testenv.NewFakeClock(t0),
	}
	e.rec = completedRecording(t, "rec-1")
	e.model = installedModel(t, "m-1")
	e.recordings = &fakeRecordings{rows: map[shared.RecordingID]*recording.Recording{e.rec.ID(): e.rec}}
	e.models = &fakeModels{rows: map[shared.SttModelID]*sttmodel.Model{e.model.ID(): e.model}}

	doc, err := transcript.NewDocument("ru", []transcript.Segment{{StartMs: 0, EndMs: 1500, Text: "Привет"}})
	if err != nil {
		t.Fatal(err)
	}
	e.doc = doc
	e.engine = testenv.NewFakeSTT(e.artifacts, doc)
	e.rebuild(t, e.engine)
	return e
}

// rebuild replaces the engine of the Service.
func (e *env) rebuild(t *testing.T, engine STTEngine) {
	t.Helper()
	svc, err := New(Deps{
		Engine: engine, Transcripts: e.transcripts, Recordings: e.recordings, Models: e.models, Artifacts: e.artifacts,
		Events: e.events, Clock: e.clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
}

func completedRecording(t *testing.T, id shared.RecordingID) *recording.Recording {
	t.Helper()
	r := startedRecording(t, id)
	at := t0.Add(time.Hour)
	if err := errors.Join(r.Stop(at), r.Finalize(at)); err != nil {
		t.Fatal(err)
	}
	track, err := r.TrackRef("wav")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Complete(at, track, time.Hour); err != nil {
		t.Fatal(err)
	}
	return r
}

func startedRecording(t *testing.T, id shared.RecordingID) *recording.Recording {
	t.Helper()
	r, err := recording.New(recording.Spec{ID: id, MaxDuration: 3 * time.Hour}, t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Begin(t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	return r
}

func installedModel(t *testing.T, id shared.SttModelID) *sttmodel.Model {
	t.Helper()
	file, err := shared.NewArtifactRef(shared.ArtifactModels, "ggml-base-q5_1.bin")
	if err != nil {
		t.Fatal(err)
	}
	m, err := sttmodel.New(sttmodel.Spec{
		ID: id, Name: "base", ModelVersion: "1", File: file, SizeBytes: 1 << 20,
		SHA256: values.SHA256("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
	}, t0.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// sttJob returns the snapshot of a claimed STT job for recording rec pinned to model.
func sttJob(t *testing.T, id shared.JobID, payload string, model shared.SttModelID) job.Snapshot {
	t.Helper()
	j, err := job.New(job.Spec{
		ID: id, Type: JobType, Key: "stt:" + string(id), Payload: payload, PinnedModelID: model, MaxAttempts: 3,
	}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Claim(t0); err != nil {
		t.Fatal(err)
	}
	return j.Snapshot()
}

func payloadFor(t *testing.T, rec shared.RecordingID) string {
	t.Helper()
	req, err := values.NewSTTRequest(rec)
	if err != nil {
		t.Fatal(err)
	}
	return req.Payload()
}

// defaultJob is job-1 for rec-1 pinned to m-1.
func (e *env) defaultJob(t *testing.T) job.Snapshot {
	t.Helper()
	return sttJob(t, "job-1", payloadFor(t, e.rec.ID()), e.model.ID())
}

// docRef is the document reference of job-1.
func docRef(t *testing.T) shared.ArtifactRef {
	t.Helper()
	ref, err := transcript.DocumentRef("job-1")
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
