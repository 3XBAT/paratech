package work

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

// answerOf builds a checked answer: feasibility f, n choice questions (default option 1), a helper unless full.
func answerOf(f Feasibility, n int) Answer {
	a := Answer{
		WorkType: TypeReferat, Requirements: Requirements{Volume: "10 страниц"}, Feasibility: f,
		Questions: []Question{}, Assumptions: []string{"Предположен вариант 3 — поправьте"},
	}
	if f != FeasibleFull {
		a.FeasibilityReason = "Нужна программа вуза"
		a.Helper = &Helper{Steps: []string{"Открыть программу", "Начертить"}, Submit: "Файл", Placeholders: []string{"ФИО"}}
	}
	for i := range n {
		a.Questions = append(a.Questions, Question{
			Kind: KindChoice, Text: "Вопрос " + string(rune('A'+i)), Options: []string{"Да", "Нет"}, DefaultOption: 1, Why: "Нужно",
		})
	}
	return a
}

func newWork(t *testing.T) *Work {
	t.Helper()
	w, err := New("w1", Source{Assignment: "a1"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func mustRecord(t *testing.T, w *Work, a Answer) {
	t.Helper()
	if err := w.Record(w.Round(), a, t0); err != nil {
		t.Fatal(err)
	}
}

// TestTransitionTable pins the status machine (ARCH-19, ARCH-62): every pair is allowed exactly as the table says.
func TestTransitionTable(t *testing.T) {
	allowed := map[[2]Status]bool{
		{StatusTriage, StatusQuestions}: true, {StatusTriage, StatusReview}: true, {StatusTriage, StatusHelper}: true,
		{StatusTriage, StatusFailed}: true, {StatusQuestions, StatusTriage}: true, {StatusReview, StatusConfirmed}: true,
		{StatusConfirmed, StatusWriting}: true, {StatusWriting, StatusDone}: true, {StatusDone, StatusWriting}: true,
	}
	for _, from := range Statuses() {
		for _, to := range Statuses() {
			if got := CanTransition(from, to); got != allowed[[2]Status{from, to}] {
				t.Errorf("%s -> %s = %v", from, to, got)
			}
		}
	}
	for _, s := range []Status{StatusHelper, StatusFailed} {
		if !s.IsTerminal() {
			t.Errorf("%s is not terminal", s)
		}
	}
	if Status("questions_3").Valid() {
		t.Error("an unknown status is valid")
	}
}

func TestNewWork(t *testing.T) {
	if _, err := New("w1", Source{Assignment: "a1", Material: "m1"}, t0); !errors.Is(err, ErrInvalidWork) {
		t.Fatalf("two sources: %v", err)
	}
	if _, err := New("w1", Source{}, t0); !errors.Is(err, ErrInvalidWork) {
		t.Fatalf("no source: %v", err)
	}
	if _, err := New("", Source{Material: "m1"}, t0); !errors.Is(err, ErrInvalidWork) {
		t.Fatalf("no id: %v", err)
	}
	w := newWork(t)
	if w.Status() != StatusTriage || w.Round() != 1 || w.Attempt() != 1 {
		t.Fatalf("new work = %+v", w.State())
	}
}

// TestTwoQuestionRoundsThenFinal: rounds 1 and 2 ask, the answers reach the reader, round 3 is final with assumptions.
func TestTwoQuestionRoundsThenFinal(t *testing.T) {
	w := newWork(t)
	mustRecord(t, w, answerOf(FeasibleFull, 2))
	if round, qs := w.Questions(); w.Status() != StatusQuestions || round != 1 || len(qs) != 2 {
		t.Fatalf("after round 1: %s %d %d", w.Status(), round, len(qs))
	}
	if err := w.Answer([]Reply{{Question: 0, Option: 0}}, t0); err != nil {
		t.Fatal(err)
	}
	if w.Status() != StatusTriage || w.Round() != 2 {
		t.Fatalf("after answers: %s round %d", w.Status(), w.Round())
	}
	req, err := w.Request("Реферат", "")
	if err != nil {
		t.Fatal(err)
	}
	// Question A was answered «Да», question B left for its default «Нет».
	if !strings.Contains(req.UntrustedData(), `{"question":"Вопрос A","answer":"Да"},{"question":"Вопрос B","answer":"Нет"}`) {
		t.Fatalf("round 2 data = %s", req.UntrustedData())
	}

	mustRecord(t, w, answerOf(FeasibleFull, 1))
	if round, _ := w.Questions(); round != 2 {
		t.Fatalf("round 2 questions: %d", round)
	}
	if err := w.Skip(t0); err != nil {
		t.Fatal(err)
	}
	if w.Round() != FinalRound || w.Status() != StatusTriage {
		t.Fatalf("after skip: %s round %d", w.Status(), w.Round())
	}
	req, err = w.Request("Реферат", "")
	if err != nil || strings.Count(req.UntrustedData(), `"question"`) != 3 {
		t.Fatalf("final data = %s, %v", req.UntrustedData(), err)
	}

	// There is no third question round: the questions of the final answer are dropped and it goes to review.
	mustRecord(t, w, answerOf(FeasibleFull, 1))
	if w.Status() != StatusReview {
		t.Fatalf("final round: %s", w.Status())
	}
	if a, _ := w.Latest(); len(a.Assumptions) != 1 || len(a.Questions) != 0 {
		t.Fatalf("final answer = %+v", a)
	}
	if err := w.ConfirmRequirements(t0); err != nil || w.Status() != StatusConfirmed {
		t.Fatalf("confirm: %v %s", err, w.Status())
	}
	b, ok := w.Brief()
	if !ok || !slices.Contains(b.Answers, Previous{Question: "Вопрос A", Answer: "Да"}) || len(b.Assumptions) != 1 {
		t.Fatalf("brief = %+v, %v", b, ok)
	}
}

func TestOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		answer  Answer
		want    Status
		partial bool
	}{
		{"full, no questions", answerOf(FeasibleFull, 0), StatusReview, false},
		{"partial with questions", answerOf(FeasiblePartial, 1), StatusQuestions, true},
		{"partial, no questions", answerOf(FeasiblePartial, 0), StatusReview, true},
		{"impossible goes to the helper even with questions", answerOf(FeasibleImpossible, 2), StatusHelper, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWork(t)
			mustRecord(t, w, tt.answer)
			if w.Status() != tt.want || w.Partial() != tt.partial {
				t.Fatalf("status %s partial %v", w.Status(), w.Partial())
			}
			if tt.want == StatusHelper {
				if _, qs := w.Questions(); qs != nil {
					t.Fatal("the helper mode shows questions")
				}
				if err := w.ConfirmRequirements(t0); !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("confirm in the helper mode: %v", err)
				}
			}
		})
	}
}

func TestRefusedAnswersFailAfterTheLastAttempt(t *testing.T) {
	w := newWork(t)
	for attempt := 1; attempt < MaxAttempts; attempt++ {
		if err := w.Refuse(1, t0); err != nil {
			t.Fatal(err)
		}
		if w.Status() != StatusTriage || w.Attempt() != attempt+1 {
			t.Fatalf("after refusal %d: %s attempt %d", attempt, w.Status(), w.Attempt())
		}
	}
	if err := w.Refuse(1, t0); err != nil || w.Status() != StatusFailed {
		t.Fatalf("last refusal: %v %s", err, w.Status())
	}
	if _, err := w.Request("a", ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("request of a failed work: %v", err)
	}
}

func TestInvalidMoves(t *testing.T) {
	w := newWork(t)
	if err := w.Record(2, answerOf(FeasibleFull, 0), t0); !errors.Is(err, ErrStaleRound) {
		t.Fatalf("stale round: %v", err)
	}
	if err := w.Refuse(2, t0); !errors.Is(err, ErrStaleRound) {
		t.Fatalf("stale refusal: %v", err)
	}
	if err := w.Answer(nil, t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("answer without questions: %v", err)
	}
	if err := w.ConfirmRequirements(t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("confirm in triage: %v", err)
	}
	if err := w.Edit(Understanding{WorkType: TypeEssay}, t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("edit in triage: %v", err)
	}
	bad := answerOf(FeasiblePartial, 0)
	bad.Helper = nil
	if err := w.Record(1, bad, t0); !errors.Is(err, ErrInvalidAnswer) {
		t.Fatalf("partial without helper: %v", err)
	}
	mustRecord(t, w, answerOf(FeasibleFull, 1))
	if err := w.Record(1, answerOf(FeasibleFull, 0), t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second answer of a round: %v", err)
	}
	for _, r := range [][]Reply{
		{{Question: 1}},
		{{Question: 0, Option: 2}},
		{{Question: 0, Text: "Да"}},
		{{Question: 0}, {Question: 0}},
	} {
		if err := w.Answer(r, t0); !errors.Is(err, ErrInvalidReply) {
			t.Errorf("replies %v: %v", r, err)
		}
	}
	if w.Status() != StatusQuestions {
		t.Fatalf("a refused answer changed the status to %s", w.Status())
	}
}

func TestResolveTextAnswers(t *testing.T) {
	qs := []Question{{Kind: KindText, Text: "Тема?", DefaultText: "Любая", Why: "w"}, {Kind: KindText, Text: "Объём?", Why: "w"}}
	got, err := Resolve(qs, []Reply{{Question: 1, Text: "  5 страниц  "}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Reply{{Question: 0, Text: "Любая", Default: true}, {Question: 1, Text: "5 страниц"}}
	if !slices.Equal(got, want) {
		t.Fatalf("resolved = %+v", got)
	}
	if _, err := Resolve(qs, []Reply{{Question: 0, Text: strings.Repeat("я", MaxAnswerRunes+1)}}); !errors.Is(err, ErrInvalidReply) {
		t.Fatalf("long answer: %v", err)
	}
	if _, err := Resolve(qs, []Reply{{Question: 0, Text: "a\x1bb"}}); !errors.Is(err, ErrInvalidReply) {
		t.Fatalf("control character: %v", err)
	}
	if d := Defaults(qs); d[1].Text != "" || !d[1].Default {
		t.Fatalf("defaults = %+v", d)
	}
}

func TestEditUnderstanding(t *testing.T) {
	w := newWork(t)
	mustRecord(t, w, answerOf(FeasibleFull, 1))
	edit := Understanding{WorkType: TypeDoklad, Requirements: Requirements{Volume: "10 страниц", Variant: " 3 "}}
	if err := w.Edit(edit, t0); err != nil {
		t.Fatal(err)
	}
	u, edited := w.Understanding()
	if u.WorkType != TypeDoklad || u.Requirements.Variant != "3" || u.Requirements.Volume != "10 страниц" ||
		!slices.Equal(edited, []Field{FieldWorkType, FieldVariant}) {
		t.Fatalf("card = %+v, edited %v", u, edited)
	}

	// The next round's answer does not overwrite the student's fields; the others follow the reader.
	if err := w.Skip(t0); err != nil {
		t.Fatal(err)
	}
	next := answerOf(FeasibleFull, 0)
	next.Requirements.Volume = "12 страниц"
	mustRecord(t, w, next)
	u, _ = w.Understanding()
	if u.WorkType != TypeDoklad || u.Requirements.Variant != "3" || u.Requirements.Volume != "12 страниц" {
		t.Fatalf("card after round 2 = %+v", u)
	}

	for _, bad := range []Understanding{
		{WorkType: "poem"},
		{WorkType: TypeEssay, Requirements: Requirements{Volume: "a\nb"}},
		{WorkType: TypeEssay, Requirements: Requirements{Sources: strings.Repeat("я", MaxRequirementRunes+1)}},
	} {
		if err := w.Edit(bad, t0); !errors.Is(err, ErrInvalidReply) {
			t.Errorf("edit %+v: %v", bad, err)
		}
	}
	if err := w.ConfirmRequirements(t0); err != nil {
		t.Fatal(err)
	}
	if err := w.Edit(edit, t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("edit after confirm: %v", err)
	}
	if b, ok := w.Brief(); !ok || b.WorkType != TypeDoklad || b.Requirements.Variant != "3" {
		t.Fatalf("brief = %+v, %v", b, ok)
	}
}

func TestAskStep(t *testing.T) {
	w := newWork(t)
	mustRecord(t, w, answerOf(FeasibleImpossible, 0))
	q, err := w.AskStep(1, "Какой масштаб?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.Data(), `"step":"Начертить"`) || strings.Contains(q.Data(), "Открыть программу") ||
		!strings.HasPrefix(q.Key(), stepKeyPrefix) {
		t.Fatalf("step question = %s %s", q.Data(), q.Key())
	}
	for _, tt := range []struct {
		step int
		q    string
	}{{2, "?"}, {-1, "?"}, {0, " "}, {0, strings.Repeat("я", MaxStepQuestionRunes+1)}} {
		if _, err := w.AskStep(tt.step, tt.q); !errors.Is(err, ErrInvalidReply) {
			t.Errorf("step %d: %v", tt.step, err)
		}
	}
	full := newWork(t)
	mustRecord(t, full, answerOf(FeasibleFull, 0))
	if _, err := full.AskStep(0, "?"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("a full work has no steps: %v", err)
	}
}

func TestRestore(t *testing.T) {
	w := newWork(t)
	mustRecord(t, w, answerOf(FeasibleFull, 1))
	if err := w.Answer(nil, t0); err != nil {
		t.Fatal(err)
	}
	st := w.State()
	st.Version = 3
	got, err := Restore(st)
	if err != nil || got.Round() != 2 || got.Status() != StatusTriage || got.Version() != 3 {
		t.Fatalf("restore: %v %+v", err, got)
	}

	breakers := []struct {
		name string
		edit func(*State)
	}{
		{"version 0", func(s *State) { s.Version = 0 }},
		{"unknown status", func(s *State) { s.Status = "lost" }},
		{"questions without questions", func(s *State) { s.Status = StatusQuestions }},
		{"round without its rounds", func(s *State) { s.Round = 3 }},
		{"unanswered earlier round", func(s *State) { s.Rounds[0].Replies = nil }},
		{"replies of another shape", func(s *State) { s.Rounds[0].Replies = []Reply{{Question: 0, Option: 5}} }},
		{"failed with attempts left", func(s *State) { s.Status = StatusFailed }},
		{"bad edit", func(s *State) { s.Edits = map[Field]string{FieldWorkType: "poem"} }},
		{"helper of a full work", func(s *State) { s.Status, s.Round = StatusHelper, 1; s.Rounds[0].Replies = nil }},
	}
	for _, tt := range breakers {
		t.Run(tt.name, func(t *testing.T) {
			st := w.State()
			st.Version = 3
			st.Rounds = w.Rounds()
			tt.edit(&st)
			if _, err := Restore(st); !errors.Is(err, ErrInvalidWork) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
