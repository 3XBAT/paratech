package recordingdelete

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/deletion"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// world is the state the fakes share: one database and one artifact root, so a second Service sees what the first one
// left (a restart after a crash).
type world struct {
	mu         sync.Mutex
	now        time.Time
	recordings map[shared.RecordingID]*recording.Recording // stored copies
	contents   map[shared.RecordingID]deletion.Contents
	docs       map[shared.RecordingID][]shared.ArtifactRef
	jobs       map[shared.JobID]*job.Job
	jobOwner   map[shared.JobID]shared.RecordingID   // jobs of a recording (directly or by a transcript)
	jobOf      map[shared.JobID]shared.AssignmentID  // draft jobs of an assignment
	running    map[shared.JobID]int                  // running jobs: looks until the handler returns; <0 never
	files      map[shared.ArtifactRef]bool           // files that exist
	deleted    []shared.ArtifactRef                  // every Delete call
	removals   []shared.AssignmentID                 // deadline event removals written by a purge
	purged     map[shared.RecordingID][]shared.JobID // the jobs a purge removed
	recovering map[shared.RecordingID]bool
	syncs      int

	failFiles   error // returned by every artifact Delete
	failPurge   error // returned by Purge
	onSleep     func()
	onDelete    func() // called, unlocked, after every artifact Delete
	cancelCalls int
}

func newWorld() *world {
	return &world{
		now: t0, recordings: map[shared.RecordingID]*recording.Recording{}, contents: map[shared.RecordingID]deletion.Contents{},
		docs: map[shared.RecordingID][]shared.ArtifactRef{}, jobs: map[shared.JobID]*job.Job{},
		jobOwner: map[shared.JobID]shared.RecordingID{}, jobOf: map[shared.JobID]shared.AssignmentID{},
		running: map[shared.JobID]int{}, files: map[shared.ArtifactRef]bool{}, purged: map[shared.RecordingID][]shared.JobID{},
		recovering: map[shared.RecordingID]bool{},
	}
}

func cloneRecording(t testing.TB, r *recording.Recording) *recording.Recording {
	t.Helper()
	d, _ := r.Duration()
	c, err := recording.Restore(recording.State{
		Version: r.Version(), ID: r.ID(), LessonID: r.LessonID(), Lesson: r.LessonSnapshot(), Status: r.Status(),
		MaxDuration: r.MaxDuration(), Duration: d, Chunks: r.Chunks(), ChunksDir: r.ChunksDir(), Audio: r.Audio(),
		Silent: r.Silent(), Problem: r.Problem(), StartedAt: r.StartedAt(), StoppedAt: r.StoppedAt(),
		HeartbeatAt: r.HeartbeatAt(), CreatedAt: r.CreatedAt(), UpdatedAt: r.UpdatedAt(),
		DeletingAt: r.DeletingAt(), DeletingAssignments: r.DeletingAssignments(),
	})
	if err != nil {
		t.Fatalf("clone %s: %v", r.ID(), err)
	}
	return c
}

func mustRef(t testing.TB, s string) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.ParseArtifactRef(s)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// addRecording stores a recording in status s (COMPLETED with its track, RECORDING, or FAILED) and its files.
func (w *world) addRecording(t *testing.T, id shared.RecordingID, s recording.Status) *recording.Recording {
	t.Helper()
	r, err := recording.New(recording.Spec{ID: id, MaxDuration: 3 * time.Hour}, t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	at := t0.Add(-time.Hour)
	steps := []func() error{func() error { return r.Begin(at) }}
	switch s {
	case recording.StatusCompleted:
		steps = append(steps, func() error { return r.Stop(at) }, func() error { return r.Finalize(at) },
			func() error { return r.ObserveChunks(2, at) },
			func() error { return r.Complete(at, mustRef(t, "recordings/"+string(id)+".wav"), time.Minute) })
	case recording.StatusFailed:
		steps = []func() error{func() error { return r.Fail(at, "device") }}
	}
	for _, step := range steps {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	r.Persisted(1)

	w.mu.Lock()
	defer w.mu.Unlock()
	w.recordings[id] = cloneRecording(t, r)
	w.files[r.ChunksDir()] = true
	if !r.Audio().IsZero() {
		w.files[r.Audio()] = true
	}
	return r
}

func (w *world) setContents(id shared.RecordingID, c deletion.Contents, docs ...shared.ArtifactRef) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.contents[id] = c
	w.docs[id] = docs
	for _, d := range docs {
		w.files[d] = true
	}
}

// addJob stores a job of the recording (or of the assignment when it is set) in status s. A RUNNING job stops looks
// looks after its cancel was asked for (never when looks < 0).
func (w *world) addJob(t *testing.T, id shared.JobID, typ job.Type, rec shared.RecordingID, a shared.AssignmentID,
	s job.Status, looks int) {
	t.Helper()
	j, err := job.New(job.Spec{ID: id, Type: typ, Key: "k-" + string(id), MaxAttempts: 3}, t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	switch s {
	case job.StatusRunning:
		err = j.Claim(t0.Add(-time.Minute))
	case job.StatusCompleted:
		if err = j.Claim(t0.Add(-time.Minute)); err == nil {
			err = j.Complete(t0.Add(-time.Minute))
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	j.Persisted(1)

	w.mu.Lock()
	defer w.mu.Unlock()
	w.jobs[id] = j
	if a != "" {
		w.jobOf[id] = a
	} else {
		w.jobOwner[id] = rec
	}
	if s == job.StatusRunning {
		w.running[id] = looks
	}
}

func (w *world) status(id shared.JobID) job.Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.jobs[id].Status()
}

func (w *world) stored(id shared.RecordingID) (*recording.Recording, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	r, ok := w.recordings[id]
	return r, ok
}

// ---- ports ----

type recordingsFake struct {
	w *world
	t testing.TB
}

func (f recordingsFake) Get(_ context.Context, id shared.RecordingID) (*recording.Recording, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	r, ok := f.w.recordings[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return cloneRecording(f.t, r), nil
}

func (f recordingsFake) Save(_ context.Context, r *recording.Recording) error {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	cur, ok := f.w.recordings[r.ID()]
	if !ok || cur.Version() != r.Version() {
		return shared.ErrVersionConflict
	}
	r.Persisted(r.Version() + 1)
	f.w.recordings[r.ID()] = cloneRecording(f.t, r)
	return nil
}

func (f recordingsFake) ListDeleting(context.Context) ([]shared.RecordingID, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	var out []shared.RecordingID
	for id, r := range f.w.recordings {
		if r.Deleting() {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out, nil
}

type storeFake struct{ w *world }

func (f storeFake) Contents(_ context.Context, id shared.RecordingID) (deletion.Contents, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	c := f.w.contents[id]
	c.Assignments = slices.Clone(c.Assignments)
	return c, nil
}

func (f storeFake) Files(_ context.Context, id shared.RecordingID) ([]shared.ArtifactRef, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	return slices.Clone(f.w.docs[id]), nil
}

func (f storeFake) Purge(_ context.Context, r *recording.Recording, assignments []shared.AssignmentID, jobs []shared.JobID,
	_ time.Time) error {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	if f.w.failPurge != nil {
		return f.w.failPurge
	}
	cur, ok := f.w.recordings[r.ID()]
	if !ok || cur.Version() != r.Version() {
		return shared.ErrVersionConflict
	}
	delete(f.w.recordings, r.ID())
	delete(f.w.contents, r.ID())
	delete(f.w.docs, r.ID())
	for _, j := range jobs {
		if f.w.jobs[j].Status().IsTerminal() {
			delete(f.w.jobs, j)
		}
	}
	f.w.purged[r.ID()] = slices.Clone(jobs)
	f.w.removals = append(f.w.removals, assignments...)
	return nil
}

type jobsFake struct{ w *world }

func (f jobsFake) ListForRecordings(_ context.Context, recs []shared.RecordingID, _, _ []job.Type) (
	map[shared.RecordingID][]*job.Job, []shared.JobID, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	out := map[shared.RecordingID][]*job.Job{}
	for id, rec := range f.w.jobOwner {
		if j, ok := f.w.jobs[id]; ok && slices.Contains(recs, rec) {
			out[rec] = append(out[rec], j)
		}
	}
	return out, nil, nil
}

func (f jobsFake) ListForAssignments(_ context.Context, ids []shared.AssignmentID, _ []job.Type) ([]*job.Job, []shared.JobID, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	var out []*job.Job
	for id, a := range f.w.jobOf {
		if j, ok := f.w.jobs[id]; ok && slices.Contains(ids, a) {
			out = append(out, j)
		}
	}
	return out, nil, nil
}

var errNotCancellable = errors.New("not cancellable now")

// cancellerFake is the job runner: a waiting job is cancelled at once, a running one when its handler returns (after
// the looks addJob gave it).
type cancellerFake struct{ w *world }

func (f cancellerFake) Cancel(_ context.Context, id shared.JobID) error {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	f.w.cancelCalls++
	j, ok := f.w.jobs[id]
	if !ok {
		return shared.ErrNotFound
	}
	if j.Status() == job.StatusRunning {
		looks, running := f.w.running[id]
		switch {
		case !running || looks < 0:
			return errNotCancellable
		case looks == 0:
			delete(f.w.running, id)
			return j.Cancel(f.w.now)
		}
		f.w.running[id] = looks - 1
		return nil
	}
	return j.Cancel(f.w.now)
}

type artifactsFake struct{ w *world }

func (f artifactsFake) Delete(_ context.Context, ref shared.ArtifactRef) error {
	f.w.mu.Lock()
	if f.w.failFiles != nil {
		f.w.mu.Unlock()
		return f.w.failFiles
	}
	f.w.deleted = append(f.w.deleted, ref)
	delete(f.w.files, ref)
	hook := f.w.onDelete
	f.w.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

type guardFake struct{ w *world }

func (f guardFake) Recovering(id shared.RecordingID) bool {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	return f.w.recovering[id]
}

type syncFake struct{ w *world }

func (f syncFake) RequestSync(context.Context) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	f.w.syncs++
}

type clockFake struct{ w *world }

func (f clockFake) Now() time.Time {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	return f.w.now
}

func (f clockFake) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.w.mu.Lock()
	f.w.now = f.w.now.Add(d)
	hook := f.w.onSleep
	f.w.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

type txFake struct{}

func (txFake) Do(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

func newService(t *testing.T, w *world) *Service {
	t.Helper()
	s, err := New(Deps{
		Recordings: recordingsFake{w: w, t: t}, Store: storeFake{w}, Jobs: jobsFake{w}, Canceller: cancellerFake{w},
		Artifacts: artifactsFake{w}, Recovery: guardFake{w}, Clock: clockFake{w}, Sleeper: clockFake{w}, Tx: txFake{},
		Sync: syncFake{w},
	}, Config{TrackExtension: "wav", Wait: 5 * time.Second, Poll: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
