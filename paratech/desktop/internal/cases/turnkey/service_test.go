package turnkey

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

// out is the canonical reader output of an answer: feasibility f, n choice questions, a helper unless full.
func out(f work.Feasibility, n int) string {
	a := work.Answer{
		WorkType: work.TypeReferat, Requirements: work.Requirements{Volume: "10 страниц"}, Feasibility: f,
		Questions: []work.Question{}, Assumptions: []string{"Предположен вариант 3 — поправьте"},
	}
	if f != work.FeasibleFull {
		a.FeasibilityReason = "Чертёж в программе вуза"
		a.Helper = &work.Helper{Steps: []string{"Открыть программу", "Начертить"}, Submit: "Файл .dwg",
			ReadyParts: []string{"Пояснительная записка"}, Placeholders: []string{"ФИО"}}
	}
	for range n {
		a.Questions = append(a.Questions, work.Question{
			Kind: work.KindChoice, Text: "Какой стиль?", Options: []string{"Научный", "Публицистический"}, DefaultOption: 1, Why: "Тон",
		})
	}
	return a.Encode()
}

func dataOf(t *testing.T, r work.Request) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal([]byte(r.UntrustedData()), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestNewChecksPorts(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(empty) = %v", err)
	}
}

// Two question rounds, answers and a skip, then the final call; the answers reach the reader of the next round.
func TestTwoRoundsThenFinal(t *testing.T) {
	e := newEnv(t)
	e.reader.then(out(work.FeasibleFull, 2)).then(out(work.FeasibleFull, 1)).then(out(work.FeasibleFull, 0))
	ctx := t.Context()

	st, err := e.svc.Triage(ctx, work.Source{Assignment: "a1"})
	if err != nil || !st.Created || st.View.Status != work.StatusQuestions || st.View.QuestionRound != 1 || len(st.View.Questions) != 2 {
		t.Fatalf("Triage = %+v, %v", st, err)
	}
	d := dataOf(t, e.reader.reqs[0])
	if d["assignment_text"] != "Реферат по истории\n\nТема на выбор" || !strings.HasSuffix(d["material_text"].(string), "Методичка: объём 10 страниц") || d["round"] != float64(1) {
		t.Fatalf("round 1 data = %v", d)
	}

	v, err := e.svc.AnswerQuestions(ctx, st.View.ID, []work.Reply{{Question: 0, Option: 0}})
	if err != nil || v.Status != work.StatusQuestions || v.QuestionRound != 2 {
		t.Fatalf("answers = %+v, %v", v, err)
	}
	if prev := dataOf(t, e.reader.reqs[1])["previous"].([]any); len(prev) != 2 ||
		prev[0].(map[string]any)["answer"] != "Научный" || prev[1].(map[string]any)["answer"] != "Публицистический" {
		t.Fatalf("round 2 previous = %v", prev)
	}

	v, err = e.svc.SkipQuestions(ctx, v.ID)
	if err != nil || v.Status != work.StatusReview || v.Round != work.FinalRound || len(v.Assumptions) != 1 {
		t.Fatalf("skip = %+v, %v", v, err)
	}
	if d := dataOf(t, e.reader.reqs[2]); d["round"] != float64(3) || len(d["previous"].([]any)) != 3 {
		t.Fatalf("final data = %v", d)
	}
	if len(v.Answered) != 3 || !v.Answered[2].Default || v.Answered[0].Default {
		t.Fatalf("answered = %+v", v.Answered)
	}

	// No question round is left: answering again is refused, nothing is paid.
	if _, err := e.svc.SkipQuestions(ctx, v.ID); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("a third round = %v", err)
	}
	if e.reader.paid() != 3 {
		t.Fatalf("paid calls = %d", e.reader.paid())
	}

	v, err = e.svc.EditUnderstanding(ctx, v.ID, work.Understanding{WorkType: work.TypeDoklad, Requirements: work.Requirements{Volume: "12 страниц"}})
	if err != nil || v.Understanding.WorkType != work.TypeDoklad || len(v.Edited) != 2 {
		t.Fatalf("edit = %+v, %v", v, err)
	}
	v, err = e.svc.ConfirmRequirements(ctx, v.ID)
	// The confirmed card reaches the writer as data (work.Brief), not as an instruction text (TASK-131).
	if err != nil || v.Status != work.StatusConfirmed || v.Writing != nil || v.Understanding.Requirements.Volume != "12 страниц" {
		t.Fatalf("confirm = %+v, %v", v, err)
	}
}

// A third question round from the reader (an answer of the final round with questions) is not a refusal: as the Hub
// does, the questions are dropped and the answer goes to review with its assumptions, without a paid retry.
func TestThirdQuestionRoundDropped(t *testing.T) {
	e := newEnv(t)
	e.reader.then(out(work.FeasibleFull, 1)).then(out(work.FeasibleFull, 1)).then(out(work.FeasibleFull, 1))
	ctx := t.Context()
	st, _ := e.svc.Triage(ctx, work.Source{Assignment: "a1"})
	v, _ := e.svc.SkipQuestions(ctx, st.View.ID)
	v, err := e.svc.SkipQuestions(ctx, v.ID)
	if err != nil || v.Status != work.StatusReview || v.Round != work.FinalRound || v.Attempt != 1 ||
		len(v.Questions) != 0 || len(v.Assumptions) != 1 {
		t.Fatalf("questions in the final round = %+v, %v", v, err)
	}
	if e.reader.paid() != 3 {
		t.Fatalf("paid calls = %d", e.reader.paid())
	}
}

func TestOutcomesOfTheFirstRound(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		status  work.Status
		partial bool
		helper  bool
	}{
		{"impossible goes to the helper", out(work.FeasibleImpossible, 1), work.StatusHelper, false, true},
		{"partial is flagged with its placeholders", out(work.FeasiblePartial, 0), work.StatusReview, true, true},
		{"full without questions", out(work.FeasibleFull, 0), work.StatusReview, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.reader.then(tt.out)
			st, err := e.svc.Triage(t.Context(), work.Source{Material: "m2"})
			v := st.View
			if err != nil || v.Status != tt.status || v.Partial != tt.partial || (v.Helper != nil) != tt.helper {
				t.Fatalf("view = %+v, %v", v, err)
			}
			if d := dataOf(t, e.reader.reqs[0]); !strings.HasSuffix(d["assignment_text"].(string), "Задание: эссе") || d["material_text"] != "" {
				t.Fatalf("upload data = %v", d)
			}
			if tt.helper && (v.Helper.Placeholders[0] != "ФИО" || len(v.Helper.Steps) != 2) {
				t.Fatalf("helper = %+v", v.Helper)
			}
		})
	}
}

// A malformed answer is a failed step: the attempt is used, after the last one the work fails. Never a panic.
func TestMalformedAnswersFailTheWork(t *testing.T) {
	e := newEnv(t)
	e.reader.then(`{"work_type":"essay"}`).then("prose, not JSON").fail(work.ErrInvalidAnswer)
	ctx := t.Context()
	st, err := e.svc.Triage(ctx, work.Source{Assignment: "a1"})
	if !errors.Is(err, ErrReaderRefused) || st.View.Status != work.StatusTriage || st.View.Attempt != 2 {
		t.Fatalf("first refusal = %+v, %v", st.View, err)
	}
	v, err := e.svc.Retry(ctx, st.View.ID)
	if !errors.Is(err, ErrReaderRefused) || v.Attempt != 3 {
		t.Fatalf("second refusal = %+v, %v", v, err)
	}
	v, err = e.svc.Retry(ctx, v.ID)
	if !errors.Is(err, ErrReaderRefused) || v.Status != work.StatusFailed {
		t.Fatalf("last refusal = %+v, %v", v, err)
	}
	if e.reader.paid() != 3 {
		t.Fatalf("keys = %d, want one per attempt", e.reader.paid())
	}
	// A failed work frees its source: a new triage starts a new work.
	e.reader.then(out(work.FeasibleFull, 0))
	again, err := e.svc.Triage(ctx, work.Source{Assignment: "a1"})
	if err != nil || !again.Created || again.View.ID == v.ID {
		t.Fatalf("after a failed work = %+v, %v", again, err)
	}
}

// No network: the work waits with its call due; the retry uses the same key (free on the Hub).
func TestReaderUnavailableKeepsTheKey(t *testing.T) {
	e := newEnv(t)
	e.reader.fail(errors.New("no network")).then(out(work.FeasibleFull, 0))
	ctx := t.Context()
	st, err := e.svc.Triage(ctx, work.Source{Assignment: "a1"})
	if !errors.Is(err, ErrReaderUnavailable) || st.View.Status != work.StatusTriage || st.View.Attempt != 1 || !st.Created {
		t.Fatalf("Triage offline = %+v, %v", st, err)
	}
	// The agent asks again: the same work, its due call asked again with the same key.
	again, err := e.svc.Triage(ctx, work.Source{Assignment: "a1"})
	if err != nil || again.Created || again.View.ID != st.View.ID || again.View.Status != work.StatusReview {
		t.Fatalf("Triage again = %+v, %v", again, err)
	}
	if e.reader.reqs[0].Key() != e.reader.reqs[1].Key() {
		t.Fatal("the retry has a new key")
	}
	// A triaged work is not asked about again.
	if third, err := e.svc.Triage(ctx, work.Source{Assignment: "a1"}); err != nil || third.Created || len(e.reader.reqs) != 2 {
		t.Fatalf("third Triage = %+v, %v (%d calls)", third, err, len(e.reader.reqs))
	}
}

func TestQuotaAndSourceErrors(t *testing.T) {
	e := newEnv(t)
	e.reader.fail(shared.ErrQuotaExceeded)
	ctx := t.Context()
	if _, err := e.svc.Triage(ctx, work.Source{Assignment: "a1"}); !errors.Is(err, ErrQuota) {
		t.Fatalf("quota = %v", err)
	}
	tests := []struct {
		src  work.Source
		want error
	}{
		{work.Source{Assignment: "gone"}, ErrSourceNotFound},
		{work.Source{Material: "m3"}, ErrNoText},
		{work.Source{Assignment: "a1", Material: "m2"}, ErrInvalidRequest},
	}
	for _, tt := range tests {
		if _, err := e.svc.Triage(ctx, tt.src); !errors.Is(err, tt.want) {
			t.Errorf("%+v: %v, want %v", tt.src, err, tt.want)
		}
	}
	if _, err := e.svc.Work(ctx, "nope"); !errors.Is(err, ErrNotFound) || !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Work(nope) = %v", err)
	}
}

func TestInvalidAnswersAreRefusedWithoutAPaidCall(t *testing.T) {
	e := newEnv(t)
	e.reader.then(out(work.FeasibleFull, 1))
	ctx := t.Context()
	st, _ := e.svc.Triage(ctx, work.Source{Assignment: "a1"})
	if _, err := e.svc.AnswerQuestions(ctx, st.View.ID, []work.Reply{{Question: 0, Option: 7}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("option out of range = %v", err)
	}
	if _, err := e.svc.ConfirmRequirements(ctx, st.View.ID); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("confirm with questions waiting = %v", err)
	}
	if e.reader.paid() != 1 {
		t.Fatalf("paid = %d", e.reader.paid())
	}
}

// Answers saved, the next call lost: the view shows the due call and the answers are kept.
func TestAnswersKeptWhenTheNextCallFails(t *testing.T) {
	e := newEnv(t)
	e.reader.then(out(work.FeasibleFull, 1)).fail(errors.New("timeout")).then(out(work.FeasibleFull, 0))
	ctx := t.Context()
	st, _ := e.svc.Triage(ctx, work.Source{Assignment: "a1"})
	v, err := e.svc.AnswerQuestions(ctx, st.View.ID, []work.Reply{{Question: 0, Option: 0}})
	if !errors.Is(err, ErrReaderUnavailable) || v.Status != work.StatusTriage || v.Round != 2 || len(v.Answered) != 1 {
		t.Fatalf("answers with a lost call = %+v, %v", v, err)
	}
	if v, err = e.svc.Retry(ctx, v.ID); err != nil || v.Status != work.StatusReview {
		t.Fatalf("retry = %+v, %v", v, err)
	}
}

// Another call recorded the round meanwhile: the slower one takes the stored work, nothing is recorded twice.
func TestConcurrentRecordTakesTheStoredWork(t *testing.T) {
	e := newEnv(t)
	e.reader.then(out(work.FeasibleFull, 0))
	ctx := t.Context()
	w, _ := work.New("w9", work.Source{Assignment: "a1"}, t0)
	if err := e.works.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	e.works.beforeSave = func() {
		other, _ := e.works.Get(ctx, "w9")
		_ = other.Record(1, mustParse(t, out(work.FeasibleImpossible, 0)), t0)
		e.works.mu.Lock()
		st := other.State()
		st.Version++
		e.works.rows["w9"] = st
		e.works.mu.Unlock()
	}
	v, err := e.svc.Retry(ctx, "w9")
	if err != nil || v.Status != work.StatusHelper {
		t.Fatalf("Retry = %+v, %v; want the stored outcome", v, err)
	}
}

func mustParse(t *testing.T, s string) work.Answer {
	t.Helper()
	a, err := work.ParseAnswer(1, s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAskStep(t *testing.T) {
	e := newEnv(t)
	e.reader.then(out(work.FeasibleImpossible, 0))
	ctx := t.Context()
	st, _ := e.svc.Triage(ctx, work.Source{Assignment: "a1"})
	e.advice.out = "  Масштаб 1:100.  "
	got, err := e.svc.AskStep(ctx, st.View.ID, 1, "Какой масштаб?")
	if err != nil || got != "Масштаб 1:100." {
		t.Fatalf("AskStep = %q, %v", got, err)
	}
	// The advisor sees one step and the requirements, nothing else of the assignment.
	data := e.advice.got[0].Data()
	if !strings.Contains(data, "Начертить") || strings.Contains(data, "Открыть программу") || strings.Contains(data, "Реферат по истории") {
		t.Fatalf("advisor data = %s", data)
	}
	if _, err := e.svc.AskStep(ctx, st.View.ID, 5, "?"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("no such step = %v", err)
	}
	e.advice.out = ""
	if _, err := e.svc.AskStep(ctx, st.View.ID, 0, "?"); !errors.Is(err, ErrReaderRefused) {
		t.Fatalf("empty advice = %v", err)
	}

	noAdvisor, _ := New(Deps{Works: e.works, Assignments: fakeAssignments{}, Materials: fakeMaterials{}, Reader: e.reader,
		Clock: fixedClock{}, IDs: &seqIDs{}})
	if _, err := noAdvisor.AskStep(ctx, st.View.ID, 0, "?"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no advisor = %v", err)
	}
}

// Undo of the agent's triage removes the work only at the version the action left; a gone work is no error.
func TestRemove(t *testing.T) {
	e := newEnv(t)
	e.reader.then(out(work.FeasibleFull, 1))
	ctx := t.Context()
	st, _ := e.svc.Triage(ctx, work.Source{Assignment: "a1"})
	if _, err := e.svc.EditUnderstanding(ctx, st.View.ID, work.Understanding{WorkType: work.TypeEssay, Requirements: work.Requirements{Volume: "10 страниц"}}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Remove(ctx, st.View.ID, st.View.Version); !errors.Is(err, ErrChanged) {
		t.Fatalf("Remove after an edit = %v", err)
	}
	if err := e.svc.Remove(ctx, st.View.ID, st.View.Version+1); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Remove(ctx, st.View.ID, 1); err != nil {
		t.Fatalf("Remove of a gone work = %v", err)
	}
	if _, err := e.svc.WorkOf(ctx, work.Source{Assignment: "a1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("WorkOf after Remove = %v", err)
	}
}
