package wails

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

var startT0 = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// startWorks keeps works by their stored state; a work of an unknown assignment is shared.ErrConflict, as the
// foreign key of the sqlite adapter.
type startWorks struct {
	mu          sync.Mutex
	rows        map[shared.WorkID]work.State
	assignments map[shared.AssignmentID]bool
}

func (f *startWorks) Get(_ context.Context, id shared.WorkID) (*work.Work, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.rows[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return work.Restore(st)
}

func (f *startWorks) ActiveOf(_ context.Context, src work.Source) (*work.Work, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, st := range f.rows {
		if st.Source == src && st.Status != work.StatusFailed {
			return work.Restore(st)
		}
	}
	return nil, shared.ErrNotFound
}

func (f *startWorks) Save(_ context.Context, w *work.Work) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := w.State()
	if stored, ok := f.rows[st.ID]; ok && stored.Version != st.Version {
		return shared.ErrVersionConflict
	}
	if st.Version == 0 && !f.assignments[st.Source.Assignment] {
		return shared.ErrConflict
	}
	st.Version++
	f.rows[st.ID] = st
	w.Persisted(st.Version)
	return nil
}

func (f *startWorks) Delete(context.Context, shared.WorkID, int64) error { return nil }

type startAssignments map[shared.AssignmentID]*assignment.Assignment

func (f startAssignments) Get(_ context.Context, id shared.AssignmentID) (*assignment.Assignment, error) {
	if a, ok := f[id]; ok {
		return a, nil
	}
	return nil, shared.ErrNotFound
}

type startMaterials struct{}

func (startMaterials) MaterialText(context.Context, shared.MaterialID) (*material.Material, material.Text, error) {
	return nil, material.Text{}, shared.ErrNotFound
}

// startReader answers the reader calls in order and counts them.
type startReader struct {
	mu      sync.Mutex
	answers []error // nil: a valid answer with one question
	calls   int
}

func (f *startReader) TriageAssignment(context.Context, work.Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.answers) > 0 {
		err := f.answers[0]
		f.answers = f.answers[1:]
		if err != nil {
			return "", err
		}
	}
	a := work.Answer{
		WorkType: work.TypeReferat, Requirements: work.Requirements{Volume: "10 страниц"}, Feasibility: work.FeasibleFull,
		Questions: []work.Question{{
			Kind: work.KindChoice, Text: "Какой стиль?", Options: []string{"Научный", "Свободный"}, DefaultOption: 1, Why: "Тон",
		}},
		Assumptions: []string{"Предположен вариант 3"},
	}
	return a.Encode(), nil
}

func (f *startReader) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type startClock struct{}

func (startClock) Now() time.Time { return startT0 }

type startIDs struct{ n int }

func (s *startIDs) NewWorkID() shared.WorkID {
	s.n++
	return shared.WorkID(fmt.Sprintf("w%d", s.n))
}

// startService is the binding over the real use case: assignment a1 exists, the reader is scripted.
func startService(t *testing.T, reader *startReader) *TurnkeyService {
	t.Helper()
	a, err := assignment.Restore(assignment.State{
		Version: 1, ID: "a1", SourceIndex: -1, Title: "Реферат по истории", Description: "Тема на выбор",
		CreatedAt: startT0, UpdatedAt: startT0,
	})
	if err != nil {
		t.Fatal(err)
	}
	core, err := turnkey.New(turnkey.Deps{
		Works:       &startWorks{rows: map[shared.WorkID]work.State{}, assignments: map[shared.AssignmentID]bool{"a1": true}},
		Assignments: startAssignments{"a1": a}, Materials: startMaterials{}, Reader: reader, Clock: startClock{},
		IDs: &startIDs{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewTurnkeyService(core, discard())
}

// «Сделать под ключ» starts the triage; a repeated click gets the same work and pays nothing again.
func TestStartForAssignmentTriagesOnce(t *testing.T) {
	reader := &startReader{}
	s := startService(t, reader)
	ctx := t.Context()

	got, err := s.StartForAssignment(ctx, "a1")
	if err != nil || got.ID == "" || got.AssignmentID != "a1" || got.Status != string(work.StatusQuestions) ||
		len(got.Questions) != 1 || got.Problem != "" {
		t.Fatalf("StartForAssignment = %+v, %v", got, err)
	}
	again, err := s.StartForAssignment(ctx, "a1")
	if err != nil || again.ID != got.ID || again.Status != got.Status {
		t.Fatalf("second click = %+v, %v", again, err)
	}
	if n := reader.count(); n != 1 {
		t.Fatalf("reader calls = %d, want 1", n)
	}
}

// No network: the work is saved with its call due and comes back with the offline problem; the next click asks again.
func TestStartForAssignmentOffline(t *testing.T) {
	reader := &startReader{answers: []error{errors.New("no network")}}
	s := startService(t, reader)
	ctx := t.Context()

	got, err := s.StartForAssignment(ctx, "a1")
	if err != nil || got.ID == "" || got.Status != string(work.StatusTriage) || got.Problem != turnkeyProblemOffline ||
		got.Message == "" {
		t.Fatalf("offline = %+v, %v", got, err)
	}
	again, err := s.StartForAssignment(ctx, "a1")
	if err != nil || again.ID != got.ID || again.Status != string(work.StatusQuestions) || again.Problem != "" {
		t.Fatalf("after the network is back = %+v, %v", again, err)
	}
	if n := reader.count(); n != 2 {
		t.Fatalf("reader calls = %d, want 2", n)
	}
}

func TestStartForAssignmentErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		id   string
		want error
	}{
		{"no such assignment", "gone", ErrTurnkeyNoSource},
		{"empty id", "", ErrTurnkeyInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := &startReader{}
			s := startService(t, reader)
			if got, err := s.StartForAssignment(t.Context(), tt.id); !errors.Is(err, tt.want) || got.ID != "" {
				t.Fatalf("StartForAssignment(%q) = %+v, %v; want %v", tt.id, got, err, tt.want)
			}
			if reader.count() != 0 {
				t.Fatalf("reader calls = %d", reader.count())
			}
		})
	}
}

// The binding passes the assignment and maps the outcome of the use case like Retry.
func TestStartForAssignmentMapping(t *testing.T) {
	pending := helperView()
	pending.Status = work.StatusTriage
	for _, tt := range []struct {
		name    string
		view    turnkey.View
		err     error
		problem string
		want    error
	}{
		{"quota keeps the work", pending, turnkey.ErrQuota, turnkeyProblemQuota, nil},
		{"refused keeps the work", pending, turnkey.ErrReaderRefused, turnkeyProblemRefused, nil},
		{"assignment gone after the work was saved", pending, turnkey.ErrSourceNotFound, turnkeyProblemGone, nil},
		{"no work and no source", turnkey.View{}, turnkey.ErrSourceNotFound, "", ErrTurnkeyNoSource},
		{"disk", turnkey.View{}, turnkey.ErrUnavailable, "", ErrTurnkeyUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeTurnkey{view: tt.view, err: tt.err}
			got, err := NewTurnkeyService(f, discard()).StartForAssignment(t.Context(), "a7")
			if !errors.Is(err, tt.want) || got.Problem != tt.problem || f.src != (work.Source{Assignment: "a7"}) {
				t.Fatalf("= %+v, %v; src %+v", got, err, f.src)
			}
		})
	}
}
