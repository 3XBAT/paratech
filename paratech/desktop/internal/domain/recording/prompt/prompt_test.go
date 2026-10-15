package prompt

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/limits"
)

var (
	t0  = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	end = t0.Add(90 * time.Minute)
)

func lessonOf() Lesson {
	return Lesson{ID: "lesson-1", Title: "History <b>", Start: t0, End: end, TimeZone: "Europe/Moscow"}
}

func newLessonPrompt(t *testing.T) *Prompt {
	t.Helper()
	p, err := NewLesson("p1", lessonOf(), t0.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newAdHoc(t *testing.T) *Prompt {
	t.Helper()
	p, err := NewAdHoc("p2", t0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// inState builds a lesson prompt that reached s by legal moves.
func inState(t *testing.T, s State) *Prompt {
	t.Helper()
	p := newLessonPrompt(t)
	steps := map[State][]func() error{
		StateOffered:   nil,
		StateStarting:  {p.BeginStart},
		StateStarted:   {p.BeginStart, p.Started},
		StateSnoozed:   {func() error { return p.Snooze(t0) }},
		StateSkipped:   {p.Skip},
		StateExpired:   {func() error { return p.Expire(end) }},
		StateWithdrawn: {func() error { return p.Snooze(t0) }, p.Withdraw},
	}
	for _, step := range steps[s] {
		if err := step(); err != nil {
			t.Fatalf("reaching %s: %v", s, err)
		}
	}
	if p.State() != s {
		t.Fatalf("state %s, want %s", p.State(), s)
	}
	return p
}

// Every action from every state: it succeeds exactly from the listed states, lands in its target, and the table
// allows what succeeds (ARCH-19, ARCH-62). Times are chosen so that only the state decides.
func TestTransitions(t *testing.T) {
	actions := []struct {
		name string
		to   State
		from []State
		do   func(p *Prompt) error
	}{
		{"begin start", StateStarting, []State{StateOffered}, func(p *Prompt) error { return p.BeginStart() }},
		{"started", StateStarted, []State{StateStarting}, func(p *Prompt) error { return p.Started() }},
		{"start failed", StateOffered, []State{StateStarting}, func(p *Prompt) error { return p.StartFailed(ProblemFailed) }},
		{"snooze", StateSnoozed, []State{StateOffered}, func(p *Prompt) error { return p.Snooze(t0) }},
		{"reshow", StateOffered, []State{StateSnoozed}, func(p *Prompt) error { return p.Reshow(t0.Add(SnoozeDelay)) }},
		{"withdraw", StateWithdrawn, []State{StateSnoozed}, func(p *Prompt) error { return p.Withdraw() }},
		{"skip", StateSkipped, []State{StateOffered, StateSnoozed}, func(p *Prompt) error { return p.Skip() }},
		{"expire", StateExpired, []State{StateOffered, StateSnoozed}, func(p *Prompt) error { return p.Expire(end) }},
	}
	for _, a := range actions {
		for _, from := range States() {
			t.Run(a.name+" from "+from.String(), func(t *testing.T) {
				p := inState(t, from)
				err := a.do(p)
				want := slices.Contains(a.from, from)
				switch {
				case want && err != nil:
					t.Fatalf("refused: %v", err)
				case want && p.State() != a.to:
					t.Fatalf("state %s, want %s", p.State(), a.to)
				case want && !CanTransition(from, a.to):
					t.Fatalf("the table does not allow %s -> %s", from, a.to)
				case !want && !errors.Is(err, ErrInvalidTransition):
					t.Fatalf("err = %v, want ErrInvalidTransition", err)
				case !want && p.State() != from:
					t.Fatalf("a refused action changed the state to %s", p.State())
				}
			})
		}
	}
}

func TestStateTable(t *testing.T) {
	keys := make([]State, 0, len(transitions))
	for s := range transitions {
		keys = append(keys, s)
	}
	if !slices.Equal(sortedStates(keys), sortedStates(States())) {
		t.Fatalf("table keys %v, want every state %v", keys, States())
	}
	for _, s := range States() {
		if !s.Valid() {
			t.Errorf("%s is not valid", s)
		}
		for _, to := range transitions[s] {
			if !to.Valid() {
				t.Errorf("%s -> unknown %s", s, to)
			}
		}
	}
	terminal := []State{StateStarted, StateSkipped, StateExpired, StateWithdrawn}
	for _, s := range States() {
		if s.IsTerminal() != slices.Contains(terminal, s) {
			t.Errorf("%s terminal = %v", s, s.IsTerminal())
		}
		if s.Visible() != (s == StateOffered || s == StateStarting) {
			t.Errorf("%s visible = %v", s, s.Visible())
		}
	}
	if State("PAUSED").Valid() || State("PAUSED").IsTerminal() {
		t.Error("an unknown state must be neither valid nor terminal")
	}
}

func sortedStates(s []State) []State {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

func TestNewLesson(t *testing.T) {
	tests := []struct {
		name string
		id   ID
		edit func(*Lesson)
		now  time.Time
		ok   bool
	}{
		{name: "before the start", id: "p1", now: t0.Add(-5 * time.Minute), ok: true},
		{name: "during the lesson", id: "p1", now: t0.Add(time.Minute), ok: true},
		{name: "at the end", id: "p1", now: end},
		{name: "no offer time", id: "p1"},
		{name: "empty id", id: "", now: t0},
		{name: "id with a slash", id: "p/1", now: t0},
		{name: "long id", id: ID(make([]byte, maxIDBytes+1)), now: t0},
		{name: "no lesson id", id: "p1", edit: func(l *Lesson) { l.ID = "" }, now: t0},
		{name: "no start", id: "p1", edit: func(l *Lesson) { l.Start = time.Time{} }, now: t0},
		{name: "end before start", id: "p1", edit: func(l *Lesson) { l.End = l.Start }, now: t0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lessonOf()
			if tt.edit != nil {
				tt.edit(&l)
			}
			p, err := NewLesson(tt.id, l, tt.now)
			if tt.ok {
				if err != nil {
					t.Fatal(err)
				}
				if p.ID() != tt.id || p.Kind() != KindLesson || p.State() != StateOffered || p.Lesson() != l ||
					p.Problem() != ProblemNone || !p.OfferedAt().Equal(tt.now) {
					t.Fatalf("prompt %+v", p)
				}
				return
			}
			if !errors.Is(err, ErrInvalidPrompt) {
				t.Fatalf("err = %v, want ErrInvalidPrompt", err)
			}
		})
	}
}

func TestNewAdHoc(t *testing.T) {
	p := newAdHoc(t)
	if p.Kind() != KindAdHoc || p.State() != StateOffered || p.Lesson() != (Lesson{}) {
		t.Fatalf("prompt %+v", p)
	}
	if _, err := NewAdHoc("p2", time.Time{}); !errors.Is(err, ErrInvalidPrompt) {
		t.Fatalf("no time: %v", err)
	}
	if _, err := NewAdHoc("", t0); !errors.Is(err, ErrInvalidPrompt) {
		t.Fatalf("no id: %v", err)
	}
}

// Snooze only a lesson prompt whose lesson still runs after the delay.
func TestSnoozeRules(t *testing.T) {
	tests := []struct {
		name string
		p    func(t *testing.T) *Prompt
		at   time.Time
		ok   bool
	}{
		{name: "lesson at the start", p: newLessonPrompt, at: t0, ok: true},
		{name: "lesson ends right after the delay", p: newLessonPrompt, at: end.Add(-SnoozeDelay - time.Second), ok: true},
		{name: "lesson ends with the delay", p: newLessonPrompt, at: end.Add(-SnoozeDelay)},
		{name: "lesson near its end", p: newLessonPrompt, at: end.Add(-time.Minute)},
		{name: "ad hoc", p: newAdHoc, at: t0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.p(t)
			if got := p.CanSnooze(tt.at); got != tt.ok {
				t.Fatalf("CanSnooze = %v, want %v", got, tt.ok)
			}
			err := p.Snooze(tt.at)
			if tt.ok {
				if err != nil || p.State() != StateSnoozed || !p.SnoozedUntil().Equal(tt.at.Add(SnoozeDelay)) {
					t.Fatalf("Snooze = %v, state %s until %v", err, p.State(), p.SnoozedUntil())
				}
				return
			}
			if !errors.Is(err, ErrSnoozeNotAllowed) || p.State() != StateOffered {
				t.Fatalf("Snooze = %v, state %s; want ErrSnoozeNotAllowed and no change", err, p.State())
			}
		})
	}
	if p := inState(t, StateStarting); p.CanSnooze(t0) {
		t.Fatal("a starting prompt cannot be snoozed")
	}
}

func TestReshowTiming(t *testing.T) {
	p := inState(t, StateSnoozed) // snoozed at t0
	if p.ReshowDue(t0.Add(SnoozeDelay - time.Second)) {
		t.Fatal("due before the delay")
	}
	if err := p.Reshow(t0.Add(SnoozeDelay - time.Second)); !errors.Is(err, ErrNotDue) || p.State() != StateSnoozed {
		t.Fatalf("early Reshow = %v, state %s", err, p.State())
	}
	if !p.ReshowDue(t0.Add(SnoozeDelay)) {
		t.Fatal("not due after the delay")
	}

	// A late poll after the lesson end expires the prompt instead of showing it.
	if p.ReshowDue(end) {
		t.Fatal("due after the lesson end")
	}
	if err := p.Reshow(end); !errors.Is(err, ErrNotDue) {
		t.Fatalf("Reshow after the end = %v", err)
	}
	if !p.ExpiryDue(end) {
		t.Fatal("a snoozed prompt must expire at the lesson end")
	}

	if err := p.Reshow(t0.Add(SnoozeDelay)); err != nil || p.State() != StateOffered || !p.SnoozedUntil().IsZero() {
		t.Fatalf("Reshow = %v, state %s until %v", err, p.State(), p.SnoozedUntil())
	}
}

func TestExpiry(t *testing.T) {
	tests := []struct {
		name string
		p    func(t *testing.T) *Prompt
		at   time.Time
		due  bool
	}{
		{name: "offered before the end", p: newLessonPrompt, at: end.Add(-time.Second)},
		{name: "offered at the end", p: newLessonPrompt, at: end, due: true},
		{name: "snoozed after the end", p: func(t *testing.T) *Prompt { return inState(t, StateSnoozed) }, at: end.Add(time.Hour), due: true},
		{name: "starting after the end", p: func(t *testing.T) *Prompt { return inState(t, StateStarting) }, at: end.Add(time.Hour)},
		{name: "ad hoc much later", p: newAdHoc, at: end.Add(24 * time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.p(t)
			from := p.State()
			if got := p.ExpiryDue(tt.at); got != tt.due {
				t.Fatalf("ExpiryDue = %v, want %v", got, tt.due)
			}
			err := p.Expire(tt.at)
			if tt.due {
				if err != nil || p.State() != StateExpired {
					t.Fatalf("Expire = %v, state %s", err, p.State())
				}
				return
			}
			if err == nil || p.State() != from {
				t.Fatalf("Expire = %v, state %s; want a refusal and no change", err, p.State())
			}
		})
	}
}

// A failed start keeps the offer with its reason; the next start clears it.
func TestFailedStartKeepsTheOffer(t *testing.T) {
	p := inState(t, StateStarting)
	if err := p.StartFailed(ProblemNone); !errors.Is(err, ErrInvalidProblem) || p.State() != StateStarting {
		t.Fatalf("StartFailed(none) = %v, state %s", err, p.State())
	}
	if err := p.StartFailed("disk on fire"); !errors.Is(err, ErrInvalidProblem) {
		t.Fatalf("StartFailed(unknown) = %v", err)
	}
	for _, problem := range Problems() {
		if err := p.StartFailed(problem); err != nil || p.State() != StateOffered || p.Problem() != problem {
			t.Fatalf("StartFailed(%s) = %v, state %s problem %s", problem, err, p.State(), p.Problem())
		}
		if err := p.BeginStart(); err != nil || p.Problem() != ProblemNone {
			t.Fatalf("retry = %v, problem %s", err, p.Problem())
		}
	}
	if err := p.BeginStart(); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("a second start while one runs = %v", err)
	}

	// An ad-hoc prompt follows the same start path.
	a := newAdHoc(t)
	for _, step := range []func() error{a.BeginStart, a.Started} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if a.State() != StateStarted {
		t.Fatalf("ad hoc state %s", a.State())
	}
}

func TestIDValidate(t *testing.T) {
	for _, id := range []ID{"p1", "prompt-12_a", "A"} {
		if err := id.Validate(); err != nil {
			t.Errorf("%q: %v", id, err)
		}
	}
	for _, id := range []ID{"", "p 1", "p\n1", "../p", "п1"} {
		if err := id.Validate(); !errors.Is(err, ErrInvalidPrompt) {
			t.Errorf("%q: err = %v, want ErrInvalidPrompt", id, err)
		}
	}
}

func TestProblemOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Problem
	}{
		{"no error", nil, ProblemNone},
		{"no capture in this build", fmt.Errorf("start: %w", recording.ErrCaptureUnavailable), ProblemUnavailable},
		{"another recording", fmt.Errorf("start: %w: r1", limits.ErrRecordingInProgress), ProblemInProgress},
		{"the chosen app is closed", fmt.Errorf("start: %w: zoom.exe", recording.ErrSourceNotRunning), ProblemSourceGone},
		{"anything else", errors.New("disk full"), ProblemFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProblemOf(tt.err); got != tt.want {
				t.Fatalf("ProblemOf = %q, want %q", got, tt.want)
			}
		})
	}
}
