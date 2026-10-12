package jobrunner

import (
	"context"
	"sync"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// memRepo is an in-memory JobRepository that follows the documented Save/Claim contract (ADR-0004) and records
// every stored state, so tests can assert the path a job took.
type memRepo struct {
	mu      sync.Mutex
	rows    map[shared.JobID]job.State
	order   []shared.JobID
	history map[shared.JobID][]stored
}

// stored is one state written to the repository.
type stored struct {
	Status    job.Status
	Attempts  int
	Delay     time.Duration // NextRunAt - UpdatedAt: the backoff chosen when the state was written
	LastError string
}

func newMemRepo() *memRepo {
	return &memRepo{rows: map[shared.JobID]job.State{}, history: map[shared.JobID][]stored{}}
}

func stateOf(j *job.Job) job.State {
	p := j.Progress()
	return job.State{
		Version: j.Version(), ID: j.ID(), Type: j.Type(), Status: j.Status(), Key: j.Key(), Payload: j.Payload(),
		PinnedModelID: j.PinnedModelID(), Attempts: j.Attempts(), MaxAttempts: j.MaxAttempts(), NextRunAt: j.NextRunAt(),
		RunNow: j.RunNow(), Percent: p.Percent(), Checkpoint: p.Checkpoint(), LastError: j.LastError(),
		CreatedAt: j.CreatedAt(), StartedAt: j.StartedAt(), FinishedAt: j.FinishedAt(), UpdatedAt: j.UpdatedAt(),
	}
}

// put stores st with the next version and records the history; the caller holds mu.
func (r *memRepo) put(st job.State) job.State {
	st.Version++
	r.rows[st.ID] = st
	r.history[st.ID] = append(r.history[st.ID], stored{
		Status: st.Status, Attempts: st.Attempts, Delay: st.NextRunAt.Sub(st.UpdatedAt), LastError: st.LastError,
	})
	return st
}

func (r *memRepo) Enqueue(_ context.Context, j *job.Job) (shared.JobID, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, st := range r.rows {
		if st.Key == j.Key() {
			return st.ID, false, nil
		}
	}
	r.order = append(r.order, j.ID())
	st := r.put(stateOf(j))
	j.Persisted(st.Version)
	return j.ID(), true, nil
}

func (r *memRepo) Get(_ context.Context, id shared.JobID) (*job.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.rows[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return job.Restore(st)
}

func (r *memRepo) Save(_ context.Context, j *job.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.rows[j.ID()]
	if !ok || cur.Version != j.Version() {
		return shared.ErrVersionConflict
	}
	st := stateOf(j)
	st.Version = cur.Version
	j.Persisted(r.put(st).Version)
	return nil
}

func (r *memRepo) Claim(_ context.Context, now time.Time, filter job.ClaimFilter) (*job.Job, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.order {
		st := r.rows[id]
		if st.Status != job.StatusQueued || !filter.Admits(st.Type, st.RunNow) {
			continue
		}
		j, err := job.Restore(st)
		if err != nil || j.Claim(now) != nil {
			continue
		}
		next := stateOf(j)
		next.Version = st.Version
		j.Persisted(r.put(next).Version)
		return j, true, nil
	}
	return nil, false, nil
}

func (r *memRepo) ListByStatus(_ context.Context, statuses ...job.Status) ([]*job.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*job.Job
	for _, id := range r.order {
		st := r.rows[id]
		for _, s := range statuses {
			if st.Status == s {
				j, err := job.Restore(st)
				if err != nil {
					return nil, err
				}
				out = append(out, j)
			}
		}
	}
	return out, nil
}

// snapshot returns the stored state of a job.
func (r *memRepo) snapshot(id shared.JobID) job.State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rows[id]
}

// path returns the statuses a job was stored in, in order.
func (r *memRepo) path(id shared.JobID) []stored {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]stored(nil), r.history[id]...)
}

// handlerFunc adapts a function to Handler.
type handlerFunc func(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error) error

func (f handlerFunc) Handle(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error) error {
	return f(ctx, snap, report)
}

// fixedJitter always returns the same fraction.
type fixedJitter float64

func (f fixedJitter) Fraction() float64 { return float64(f) }
