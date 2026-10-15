package recordingsession

import (
	"context"
	"fmt"
	"sync"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// stateOf copies the stored form of a recording, so the fake repository never shares an instance with the service.
func stateOf(r *recording.Recording) recording.State {
	d, _ := r.Duration()
	return recording.State{
		Version: r.Version(), ID: r.ID(), LessonID: r.LessonID(), Status: r.Status(), MaxDuration: r.MaxDuration(),
		Duration: d, Chunks: r.Chunks(), ChunksDir: r.ChunksDir(), Audio: r.Audio(), Problem: r.Problem(),
		StartedAt: r.StartedAt(), StoppedAt: r.StoppedAt(), HeartbeatAt: r.HeartbeatAt(),
		CreatedAt: r.CreatedAt(), UpdatedAt: r.UpdatedAt(),
	}
}

// memRepo is a RecordingRepository with the compare-and-swap rules of ADR-0004(d).
type memRepo struct {
	mu        sync.Mutex
	rows      map[shared.RecordingID]recording.State
	history   []recording.Status // statuses of successful saves, in order
	failSaves []error            // errors returned by the next saves, one each
	saves     int                // Save calls, failed ones included
	listErr   error              // returned by ListByStatus when set
}

func newMemRepo() *memRepo { return &memRepo{rows: map[shared.RecordingID]recording.State{}} }

func (m *memRepo) failNextSaves(errs ...error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failSaves = append(m.failSaves, errs...)
}

func (m *memRepo) Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.rows[id]
	if !ok {
		return nil, fmt.Errorf("%w: recording %s", shared.ErrNotFound, id)
	}
	return recording.Restore(st)
}

func (m *memRepo) Save(_ context.Context, r *recording.Recording) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves++
	if len(m.failSaves) > 0 {
		err := m.failSaves[0]
		m.failSaves = m.failSaves[1:]
		return err
	}

	stored, exists := m.rows[r.ID()]
	switch {
	case r.Version() == 0 && exists:
		return shared.ErrVersionConflict
	case r.Version() != 0 && (!exists || stored.Version != r.Version()):
		return shared.ErrVersionConflict
	}
	st := stateOf(r)
	st.Version = r.Version() + 1
	m.rows[r.ID()] = st
	m.history = append(m.history, r.Status())
	r.Persisted(st.Version)
	return nil
}

func (m *memRepo) ListByStatus(_ context.Context, statuses ...recording.Status) ([]*recording.Recording, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	var out []*recording.Recording
	for _, st := range m.rows {
		for _, s := range statuses {
			if st.Status == s {
				r, err := recording.Restore(st)
				if err != nil {
					return nil, err
				}
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func (m *memRepo) Delete(_ context.Context, r *recording.Recording) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.rows[r.ID()]; !ok || st.Version != r.Version() {
		return shared.ErrVersionConflict
	}
	delete(m.rows, r.ID())
	return nil
}

// put stores a recording as another writer would, bumping the version.
func (m *memRepo) put(r *recording.Recording) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := stateOf(r)
	st.Version = m.rows[r.ID()].Version + 1
	m.rows[r.ID()] = st
	r.Persisted(st.Version)
}

func (m *memRepo) statuses() []recording.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recording.Status(nil), m.history...)
}

func (m *memRepo) saveCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saves
}

func (m *memRepo) statusOf(id shared.RecordingID) recording.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rows[id].Status
}

func (m *memRepo) has(id shared.RecordingID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.rows[id]
	return ok
}

// memTranscripts is a RecordingTranscripts with the transcript files of each recording.
type memTranscripts struct {
	mu      sync.Mutex
	files   map[shared.RecordingID][]shared.ArtifactRef
	deletes int
}

func newMemTranscripts() *memTranscripts {
	return &memTranscripts{files: map[shared.RecordingID][]shared.ArtifactRef{}}
}

func (m *memTranscripts) ListArtifactsByRecording(_ context.Context, id shared.RecordingID) ([]shared.ArtifactRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]shared.ArtifactRef(nil), m.files[id]...), nil
}

func (m *memTranscripts) DeleteByRecording(_ context.Context, id shared.RecordingID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, id)
	m.deletes++
	return nil
}

// fakeTx runs the function directly; before, when set, runs first, like a writer that came in between.
type fakeTx struct{ before func() }

func (f *fakeTx) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if f.before != nil {
		f.before()
	}
	return fn(ctx)
}

// hookRecorder adds to the fake recorder what it cannot script: a Stop that hangs until its context ends and
// Finalize errors for the next calls only.
type hookRecorder struct {
	*testenv.FakeRecorder

	mu            sync.Mutex
	stopHang      chan struct{} // closed when a hanging Stop is entered; nil = Stop does not hang
	stopCause     error         // context.Cause seen by the hanging Stop
	finalizeFails []error       // errors returned by the next Finalize calls, one each
	finalizes     int           // Finalize calls, failed ones included
}

// hangStops makes the next Stop calls block until their context ends; the channel closes when the first one enters.
func (h *hookRecorder) hangStops() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopHang = make(chan struct{})
	return h.stopHang
}

func (h *hookRecorder) Stop(ctx context.Context, id shared.RecordingID) error {
	h.mu.Lock()
	entered := h.stopHang
	if entered != nil {
		select {
		case <-entered:
		default:
			close(entered)
		}
	}
	h.mu.Unlock()
	if entered == nil {
		return h.FakeRecorder.Stop(ctx, id)
	}

	<-ctx.Done()
	h.mu.Lock()
	h.stopCause = context.Cause(ctx)
	h.mu.Unlock()
	return ctx.Err()
}

func (h *hookRecorder) stopCauseSeen() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopCause
}

func (h *hookRecorder) failFinalizes(errs ...error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.finalizeFails = append(h.finalizeFails, errs...)
}

func (h *hookRecorder) Finalize(ctx context.Context, chunks, output shared.ArtifactRef) (time.Duration, error) {
	h.mu.Lock()
	h.finalizes++
	var err error
	if len(h.finalizeFails) > 0 {
		err, h.finalizeFails = h.finalizeFails[0], h.finalizeFails[1:]
	}
	h.mu.Unlock()

	if err != nil {
		return 0, err
	}
	return h.FakeRecorder.Finalize(ctx, chunks, output)
}

func (h *hookRecorder) finalizeCalls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.finalizes
}

// seqIDs hands out rec-1, rec-2, ...
type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (s *seqIDs) NewRecordingID() shared.RecordingID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return shared.RecordingID(fmt.Sprintf("rec-%d", s.n))
}
