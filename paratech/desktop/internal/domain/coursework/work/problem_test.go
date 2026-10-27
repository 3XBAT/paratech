package work

import (
	"errors"
	"testing"
	"time"
)

// TASK-226: the problem of the last reader call is kept while the call is due and dropped by a recorded answer.
func TestProblemOfTheReaderCall(t *testing.T) {
	tests := []struct {
		name   string
		moves  func(t *testing.T, w *Work)
		status Status
		want   Problem
	}{
		{"none before a call", func(*testing.T, *Work) {}, StatusTriage, ""},
		{"no connection", func(t *testing.T, w *Work) { mustNote(t, w, ProblemOffline) }, StatusTriage, ProblemOffline},
		{"a later problem replaces it", func(t *testing.T, w *Work) {
			mustNote(t, w, ProblemOffline)
			mustNote(t, w, ProblemQuota)
		}, StatusTriage, ProblemQuota},
		{"a refused answer", func(t *testing.T, w *Work) { mustRefuse(t, w) }, StatusTriage, ProblemRefused},
		{"the last refused attempt fails the work", func(t *testing.T, w *Work) {
			for range MaxAttempts {
				mustRefuse(t, w)
			}
		}, StatusFailed, ProblemRefused},
		{"a recorded answer clears it", func(t *testing.T, w *Work) {
			mustNote(t, w, ProblemNoText)
			mustRecord(t, w, answerOf(FeasibleFull, 0))
		}, StatusReview, ""},
		{"questions clear it, the next round starts clean", func(t *testing.T, w *Work) {
			mustRefuse(t, w)
			mustRecord(t, w, answerOf(FeasibleFull, 1))
			if err := w.Skip(t0); err != nil {
				t.Fatal(err)
			}
		}, StatusTriage, ""},
		{"the helper mode clears it", func(t *testing.T, w *Work) {
			mustNote(t, w, ProblemSourceGone)
			mustRecord(t, w, answerOf(FeasibleImpossible, 0))
		}, StatusHelper, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWork(t)
			tt.moves(t, w)
			if w.Status() != tt.status || w.LastProblem() != tt.want {
				t.Fatalf("status %s, problem %q; want %s, %q", w.Status(), w.LastProblem(), tt.status, tt.want)
			}
			st := w.State()
			st.Version = 1
			if got, err := Restore(st); err != nil || got.LastProblem() != tt.want {
				t.Fatalf("Restore = %v, %v", got, err)
			}
		})
	}
}

func TestNoteProblemRefused(t *testing.T) {
	w := newWork(t)
	if err := w.NoteProblem("teapot", t0); !errors.Is(err, ErrInvalidWork) || w.LastProblem() != "" {
		t.Fatalf("unknown problem = %v, kept %q", err, w.LastProblem())
	}
	mustRecord(t, w, answerOf(FeasibleFull, 0))
	if err := w.NoteProblem(ProblemOffline, t0); !errors.Is(err, ErrInvalidTransition) || w.LastProblem() != "" {
		t.Fatalf("no call due = %v, kept %q", err, w.LastProblem())
	}

	later := t0.Add(time.Hour)
	f := newWork(t)
	if err := f.NoteProblem(ProblemQuota, later); err != nil || !f.UpdatedAt().Equal(later) || f.Attempt() != 1 {
		t.Fatalf("NoteProblem = %v, updated %v, attempt %d", err, f.UpdatedAt(), f.Attempt())
	}
}

func TestRestoreRefusesAProblemOutOfPlace(t *testing.T) {
	w := newWork(t)
	mustRecord(t, w, answerOf(FeasibleFull, 0))
	for name, edit := range map[string]func(*State){
		"a problem of a reviewed work": func(s *State) { s.Problem = ProblemOffline },
		"an unknown problem":           func(s *State) { s.Status, s.Rounds, s.Problem = StatusTriage, nil, "teapot" },
		"a send of a work of a file":   func(s *State) { s.Source, s.SentAt = Source{Material: "m1"}, t0 },
	} {
		t.Run(name, func(t *testing.T) {
			st := w.State()
			st.Version = 1
			edit(&st)
			if _, err := Restore(st); !errors.Is(err, ErrInvalidWork) {
				t.Fatalf("Restore = %v", err)
			}
		})
	}
}

// TASK-226: the last send time only moves forward, and only a work of an assignment has one.
func TestRecordSent(t *testing.T) {
	w := newWork(t)
	if !w.SentAt().IsZero() {
		t.Fatal("a new work was sent")
	}
	first, second := t0.Add(time.Hour), t0.Add(2*time.Hour)
	steps := []struct {
		at      time.Time
		changed bool
		want    time.Time
	}{
		{time.Time{}, false, time.Time{}}, // no send yet
		{first, true, first},
		{first, false, first}, // the same delivery read again
		{t0, false, first},    // an older send
		{second, true, second},
	}
	for i, s := range steps {
		changed, err := w.RecordSent(s.at)
		if err != nil || changed != s.changed || !w.SentAt().Equal(s.want) {
			t.Fatalf("step %d: changed %v, %v, sent %v", i, changed, err, w.SentAt())
		}
	}
	if !w.UpdatedAt().Equal(t0) {
		t.Fatalf("a send changed UpdatedAt to %v", w.UpdatedAt())
	}

	st := w.State()
	st.Version = 2
	if got, err := Restore(st); err != nil || !got.SentAt().Equal(second) {
		t.Fatalf("Restore = %v, %v", got, err)
	}

	file, err := New("w2", Source{Material: "m1"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.RecordSent(first); !errors.Is(err, ErrNoAssignment) || !file.SentAt().IsZero() {
		t.Fatalf("a work of a file = %v", err)
	}
}

func mustNote(t *testing.T, w *Work, p Problem) {
	t.Helper()
	if err := w.NoteProblem(p, t0); err != nil {
		t.Fatal(err)
	}
}

func mustRefuse(t *testing.T, w *Work) {
	t.Helper()
	if err := w.Refuse(w.Round(), t0); err != nil {
		t.Fatal(err)
	}
}
