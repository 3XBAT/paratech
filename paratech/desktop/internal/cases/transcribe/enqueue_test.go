package transcribe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/modelselection"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

// txKey marks a context that runs inside fakeEnqueueTx.Do.
type txKey struct{}

func inTx(ctx context.Context) bool { return ctx.Value(txKey{}) != nil }

type fakeEnqueueTx struct{ calls int }

func (f *fakeEnqueueTx) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	f.calls++
	return fn(context.WithValue(ctx, txKey{}, true))
}

// fakeJobs stores jobs by key; Enqueue and HasKeyPrefix must run inside the transaction.
type fakeJobs struct {
	mu         sync.Mutex
	byKey      map[string]*job.Job
	enqueueErr error
	outsideTx  int
}

func newFakeJobs() *fakeJobs { return &fakeJobs{byKey: map[string]*job.Job{}} }

func (f *fakeJobs) Enqueue(ctx context.Context, j *job.Job) (shared.JobID, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !inTx(ctx) {
		f.outsideTx++
	}
	if f.enqueueErr != nil {
		return "", false, f.enqueueErr
	}
	if old, ok := f.byKey[j.Key()]; ok {
		return old.ID(), false, nil
	}
	f.byKey[j.Key()] = j
	return j.ID(), true, nil
}

func (f *fakeJobs) HasKeyPrefix(ctx context.Context, typ job.Type, prefix string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !inTx(ctx) {
		f.outsideTx++
	}
	for k, j := range f.byKey {
		if j.Type() == typ && strings.HasPrefix(k, prefix) {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeJobs) all() []*job.Job {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*job.Job, 0, len(f.byKey))
	for _, j := range f.byKey {
		out = append(out, j)
	}
	slices.SortFunc(out, func(a, b *job.Job) int { return strings.Compare(a.Key(), b.Key()) })
	return out
}

// fakeResolver returns model or err and records whether it ran inside the transaction.
type fakeResolver struct {
	model     shared.SttModelID
	err       error
	calls     int
	outsideTx int
}

func (f *fakeResolver) ResolveForJob(ctx context.Context, requested shared.SttModelID) (shared.SttModelID, error) {
	f.calls++
	if !inTx(ctx) {
		f.outsideTx++
	}
	if requested != "" {
		return "", errors.New("the enqueue must not request a model")
	}
	return f.model, f.err
}

// fakePending lists the ids of recordings in id order and records each page request.
type fakePending struct {
	ids   []shared.RecordingID
	pages []string // "after/limit" per call
	err   error
}

func (f *fakePending) ListUntranscribed(_ context.Context, after shared.RecordingID, limit int) ([]shared.RecordingID, error) {
	f.pages = append(f.pages, fmt.Sprintf("%s/%d", after, limit))
	if f.err != nil {
		return nil, f.err
	}
	var out []shared.RecordingID
	for _, id := range f.ids {
		if id > after && len(out) < limit {
			out = append(out, id)
		}
	}
	return out, nil
}

type seqJobIDs struct{ n int }

func (s *seqJobIDs) NewJobID() shared.JobID {
	s.n++
	return shared.JobID(fmt.Sprintf("job-%d", s.n))
}

// enqueueEnv is an Enqueuer on fakes with one completed recording "rec-1" and the model "m-1".
type enqueueEnv struct {
	enq        *Enqueuer
	recordings *fakeRecordings
	pending    *fakePending
	models     *fakeResolver
	jobs       *fakeJobs
	tx         *fakeEnqueueTx
	logs       *bytes.Buffer
}

func newEnqueueEnv(t *testing.T) *enqueueEnv {
	t.Helper()
	e := &enqueueEnv{
		recordings: &fakeRecordings{rows: map[shared.RecordingID]*recording.Recording{"rec-1": completedRecording(t, "rec-1")}},
		pending:    &fakePending{}, models: &fakeResolver{model: "m-1"}, jobs: newFakeJobs(), tx: &fakeEnqueueTx{},
		logs: &bytes.Buffer{},
	}
	enq, err := NewEnqueuer(EnqueueDeps{
		Recordings: e.recordings, Pending: e.pending, Models: e.models, Jobs: e.jobs, IDs: &seqJobIDs{}, Tx: e.tx,
		Clock: testenv.NewFakeClock(t0), Log: slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		MaxAttempts: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.enq = enq
	return e
}

func TestNewEnqueuerRequiresPorts(t *testing.T) {
	full := EnqueueDeps{
		Recordings: &fakeRecordings{}, Pending: &fakePending{}, Models: &fakeResolver{}, Jobs: newFakeJobs(),
		IDs: &seqJobIDs{}, Tx: &fakeEnqueueTx{}, Clock: testenv.NewFakeClock(t0),
	}
	tests := []struct {
		name string
		drop func(d *EnqueueDeps)
	}{
		{"recordings", func(d *EnqueueDeps) { d.Recordings = nil }},
		{"pending", func(d *EnqueueDeps) { d.Pending = nil }},
		{"models", func(d *EnqueueDeps) { d.Models = nil }},
		{"jobs", func(d *EnqueueDeps) { d.Jobs = nil }},
		{"ids", func(d *EnqueueDeps) { d.IDs = nil }},
		{"tx", func(d *EnqueueDeps) { d.Tx = nil }},
		{"clock", func(d *EnqueueDeps) { d.Clock = nil }},
		{"negative attempts", func(d *EnqueueDeps) { d.MaxAttempts = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := full
			tt.drop(&d)
			if _, err := NewEnqueuer(d); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("NewEnqueuer = %v, want ErrInvalidConfig", err)
			}
		})
	}
	if _, err := NewEnqueuer(full); err != nil {
		t.Fatalf("NewEnqueuer with every port: %v", err)
	}
}

// The job of a completed recording pins the resolved model, has the key stt:<recording>:<model> and the payload of
// the recording; the model is chosen and the job stored in one transaction.
func TestRecordingCompletedEnqueuesThePinnedJob(t *testing.T) {
	e := newEnqueueEnv(t)
	if err := e.enq.RecordingCompleted(context.Background(), "rec-1"); err != nil {
		t.Fatal(err)
	}

	jobs := e.jobs.all()
	if len(jobs) != 1 {
		t.Fatalf("%d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	req, err := values.ParseSTTPayload(j.Payload())
	if err != nil {
		t.Fatal(err)
	}
	if j.ID() != "job-1" || j.Type() != JobType || j.Key() != "stt:rec-1:m-1" || j.PinnedModelID() != "m-1" ||
		j.Status() != job.StatusQueued || j.MaxAttempts() != 4 || req.Recording() != "rec-1" || !j.CreatedAt().Equal(t0) {
		t.Fatalf("job %s %s key %q pin %q status %s attempts %d payload %q", j.ID(), j.Type(), j.Key(), j.PinnedModelID(),
			j.Status(), j.MaxAttempts(), j.Payload())
	}
	if e.tx.calls != 1 || e.models.outsideTx != 0 || e.jobs.outsideTx != 0 {
		t.Fatalf("%d transactions, %d resolves and %d job calls outside it; want everything in one", e.tx.calls,
			e.models.outsideTx, e.jobs.outsideTx)
	}
	if !strings.Contains(e.logs.String(), "transcription job enqueued") {
		t.Fatalf("log lacks the enqueue line:\n%s", e.logs)
	}
}

// Test step 3: a repeated completion event creates no second job, also when the default model changed meanwhile.
func TestRepeatedCompletionCreatesOneJob(t *testing.T) {
	tests := []struct {
		name   string
		second shared.SttModelID // the model resolved for the repeated event
	}{
		{"same model", "m-1"},
		{"default changed", "m-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnqueueEnv(t)
			ctx := context.Background()
			if err := e.enq.RecordingCompleted(ctx, "rec-1"); err != nil {
				t.Fatal(err)
			}
			e.models.model = tt.second
			for range 2 {
				if err := e.enq.RecordingCompleted(ctx, "rec-1"); err != nil {
					t.Fatal(err)
				}
			}
			if jobs := e.jobs.all(); len(jobs) != 1 || jobs[0].Key() != "stt:rec-1:m-1" {
				t.Fatalf("jobs %v, want only the first one", jobs)
			}
		})
	}
}

// Only a recording the aggregate calls transcribable gets a job; a recording that is gone is not an error.
func TestOnlyTranscribableRecordingsGetAJob(t *testing.T) {
	tests := []struct {
		name string
		id   shared.RecordingID
		rec  func(t *testing.T) *recording.Recording
	}{
		{"still recording", "rec-2", func(t *testing.T) *recording.Recording { return startedRecording(t, "rec-2") }},
		{"failed", "rec-2", func(t *testing.T) *recording.Recording {
			r, err := recording.New(recording.Spec{ID: "rec-2", MaxDuration: time.Hour}, t0)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Fail(t0, "no device"); err != nil {
				t.Fatal(err)
			}
			return r
		}},
		{"deleted", "rec-gone", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnqueueEnv(t)
			if tt.rec != nil {
				e.recordings.rows[tt.id] = tt.rec(t)
			}
			if err := e.enq.RecordingCompleted(context.Background(), tt.id); err != nil {
				t.Fatalf("RecordingCompleted = %v, want nil", err)
			}
			if len(e.jobs.all()) != 0 || e.models.calls != 0 {
				t.Fatalf("%d jobs after %d model lookups, want none", len(e.jobs.all()), e.models.calls)
			}
		})
	}
}

// Without a model to pin nothing is created, the recording is fine (nil) and the log says why.
func TestTranscriptionWaitsForAModel(t *testing.T) {
	tests := []struct {
		name string
		err  error
		line string
	}{
		{"nothing installed", fmt.Errorf("resolve: %w", modelselection.ErrModelNotInstalled), "transcription waits: no speech recognition model installed"},
		{"no default", fmt.Errorf("resolve: %w", modelselection.ErrNoDefaultModel), "transcription waits: no default speech recognition model selected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnqueueEnv(t)
			e.models.err = tt.err
			if err := e.enq.RecordingCompleted(context.Background(), "rec-1"); err != nil {
				t.Fatalf("RecordingCompleted = %v, want nil", err)
			}
			if len(e.jobs.all()) != 0 {
				t.Fatal("a job was created without a model")
			}
			if out := e.logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, tt.line) {
				t.Fatalf("log lacks the warning %q:\n%s", tt.line, out)
			}
		})
	}
}

// Storage errors are returned (the caller logs them), and nothing is stored.
func TestEnqueueErrors(t *testing.T) {
	boom := errors.New("database busy")
	tests := []struct {
		name  string
		setup func(e *enqueueEnv)
	}{
		{"recording read", func(e *enqueueEnv) { e.recordings.err = boom }},
		{"model lookup", func(e *enqueueEnv) { e.models.err = boom }},
		{"job store", func(e *enqueueEnv) { e.jobs.enqueueErr = boom }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnqueueEnv(t)
			tt.setup(e)
			if err := e.enq.RecordingCompleted(context.Background(), "rec-1"); !errors.Is(err, boom) {
				t.Fatalf("RecordingCompleted = %v, want the storage error", err)
			}
			if len(e.jobs.all()) != 0 {
				t.Fatal("a job was stored")
			}
		})
	}
}

// The startup sweep pages through the untranscribed recordings and enqueues each one that has no STT job yet.
func TestEnqueuePendingSweepsEveryPage(t *testing.T) {
	e := newEnqueueEnv(t)
	var ids []shared.RecordingID
	for i := range sweepPage + 5 {
		id := shared.RecordingID(fmt.Sprintf("rec-%03d", i))
		ids = append(ids, id)
		e.recordings.rows[id] = completedRecording(t, id)
	}
	e.pending.ids = ids
	if err := e.enq.RecordingCompleted(context.Background(), ids[0]); err != nil { // already has its job
		t.Fatal(err)
	}

	n, err := e.enq.EnqueuePending(context.Background())
	if err != nil || n != len(ids)-1 {
		t.Fatalf("EnqueuePending = %d, %v; want %d new jobs", n, err, len(ids)-1)
	}
	if want := []string{fmt.Sprintf("/%d", sweepPage), fmt.Sprintf("%s/%d", ids[sweepPage-1], sweepPage)}; !slices.Equal(e.pending.pages, want) {
		t.Fatalf("pages %v, want %v", e.pending.pages, want)
	}
	if again, err := e.enq.EnqueuePending(context.Background()); err != nil || again != 0 {
		t.Fatalf("second sweep = %d, %v; want nothing new", again, err)
	}
	if len(e.jobs.all()) != len(ids) {
		t.Fatalf("%d jobs, want one per recording", len(e.jobs.all()))
	}
}

// Without a model the sweep stops at the first recording with one warning; a failing recording does not stop the
// others; a failed listing is the error.
func TestEnqueuePendingFailures(t *testing.T) {
	t.Run("no model", func(t *testing.T) {
		e := newEnqueueEnv(t)
		e.recordings.rows["rec-2"] = completedRecording(t, "rec-2")
		e.pending.ids = []shared.RecordingID{"rec-1", "rec-2"}
		e.models.err = modelselection.ErrModelNotInstalled
		if n, err := e.enq.EnqueuePending(context.Background()); err != nil || n != 0 {
			t.Fatalf("EnqueuePending = %d, %v", n, err)
		}
		if e.models.calls != 1 || strings.Count(e.logs.String(), "transcription waits") != 1 {
			t.Fatalf("%d model lookups, log:\n%s", e.models.calls, e.logs)
		}
	})
	t.Run("one recording fails", func(t *testing.T) {
		e := newEnqueueEnv(t)
		e.recordings.rows["rec-2"] = completedRecording(t, "rec-2")
		e.pending.ids = []shared.RecordingID{"rec-0", "rec-1", "rec-2"}
		boom := errors.New("row unreadable")
		e.enq.recordings = &failingRecordings{fakeRecordings: e.recordings, failID: "rec-0", err: boom}
		n, err := e.enq.EnqueuePending(context.Background())
		if !errors.Is(err, boom) || n != 2 || !strings.Contains(err.Error(), "1 recordings") {
			t.Fatalf("EnqueuePending = %d, %v; want 2 jobs and the error of one recording", n, err)
		}
	})
	t.Run("listing fails", func(t *testing.T) {
		e := newEnqueueEnv(t)
		boom := errors.New("database busy")
		e.pending.err = boom
		if _, err := e.enq.EnqueuePending(context.Background()); !errors.Is(err, boom) {
			t.Fatalf("EnqueuePending = %v, want the listing error", err)
		}
	})
}

// failingRecordings fails the read of one recording.
type failingRecordings struct {
	*fakeRecordings
	failID shared.RecordingID
	err    error
}

func (f *failingRecordings) Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error) {
	if id == f.failID {
		return nil, f.err
	}
	return f.fakeRecordings.Get(ctx, id)
}
