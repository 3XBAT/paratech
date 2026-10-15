package testenv

import (
	"context"
	"fmt"
	"sync"

	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/shared"
)

// MemDrafts is an in-memory draft repository (homeworkflow.DraftRepository) until the sqlite one exists. Drafts are
// kept as State and rebuilt with Restore, as an adapter does, and Save is the version compare-and-swap of
// ADR-0004(d).
type MemDrafts struct {
	mu      sync.Mutex
	drafts  map[shared.AssignmentID]homeworkdraft.State
	saves   int
	getErr  error // returned once by the next Get
	saveErr error // returned once by the next Save
}

// NewMemDrafts returns an empty repository.
func NewMemDrafts() *MemDrafts {
	return &MemDrafts{drafts: map[shared.AssignmentID]homeworkdraft.State{}}
}

// Get loads the draft of an assignment; none yields shared.ErrNotFound.
func (m *MemDrafts) Get(ctx context.Context, a shared.AssignmentID) (*homeworkdraft.Draft, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.getErr; err != nil {
		m.getErr = nil
		return nil, err
	}
	st, ok := m.drafts[a]
	if !ok {
		return nil, fmt.Errorf("draft of %s: %w", a, shared.ErrNotFound)
	}
	return homeworkdraft.Restore(st)
}

// Save stores a draft with the version compare-and-swap; a draft without versions is refused, as Restore would.
func (m *MemDrafts) Save(ctx context.Context, d *homeworkdraft.Draft) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.saveErr; err != nil {
		m.saveErr = nil
		return err
	}
	stored, exists := m.drafts[d.Assignment()]
	if (d.Version() == 0) == exists || (exists && stored.Version != d.Version()) {
		return shared.ErrVersionConflict
	}
	st := d.State()
	if len(st.Revisions) == 0 {
		return fmt.Errorf("draft of %s has no versions: %w", d.Assignment(), homeworkdraft.ErrInvalidDraft)
	}
	st.Version++
	m.drafts[d.Assignment()] = st
	m.saves++
	d.Persisted(st.Version)
	return nil
}

// FailNextGet makes the next Get return err.
func (m *MemDrafts) FailNextGet(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getErr = err
}

// FailNextSave makes the next Save return err.
func (m *MemDrafts) FailNextSave(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveErr = err
}

// Saves returns the number of successful saves.
func (m *MemDrafts) Saves() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saves
}
