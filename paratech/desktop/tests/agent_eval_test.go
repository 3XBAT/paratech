package tests

// Agent checks, level 1 (TASK-135, design §3.3, ADR-0021, ADR-0022): the policy rules on the real agent use cases,
// the real SQLite and the real Hub client; only the Hub is a fake. The test plays the Driver (TASK-117) only as far
// as reporting a result per call; nothing here runs a tool.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// evalEndText ends every scripted request; its card is not part of what a check expects.
const evalEndText = "Конец проверки."

var evalEnd = testenv.AgentCall{ID: "toolu_end", Tool: "reply", Args: `{"text":"` + evalEndText + `"}`}

// evalCost is a paid call below the plan threshold (D8), so one paid job may run without a plan.
const evalCost = 10_000

// evalFacts is what the Driver will know about a call: a fixed cost and the tools that would replace a ready result.
type evalFacts struct {
	cost     int64
	replaces map[values.Tool]bool
}

func (f evalFacts) Facts(_ context.Context, c values.Call) (thread.Facts, error) {
	return thread.Facts{CostMicroUSD: f.cost, ReplacesReady: f.replaces[c.Tool()]}, nil
}

// newEvalEnv is newAgentEnv with the given call facts.
func newEvalEnv(t *testing.T, facts agentturn.CallFacts) *agentEnv {
	t.Helper()
	e := newAgentEnv(t)
	s1 := agentRef(t, values.KindSubject, "s1")
	var err error
	if e.turns, err = agentturn.New(agentturn.Deps{
		Model: e.model, Threads: sqlite.NewAgentThreadRepository(e.db), History: sqlite.NewAgentHistory(e.db, e.clock), Subjects: baseSubjects{{ID: s1, Name: "Физика"}},
		Facts: facts, Tx: e.db, Clock: e.clock, IDs: e.ids,
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

// evalScope is the context of every eval request: subject s1, lesson l1 and assignment a1 on the assignments screen.
func evalScope(t *testing.T) values.Scope {
	t.Helper()
	sc, err := values.NewScope(agentRef(t, values.KindSubject, "s1"), values.ScreenAssignments,
		agentRef(t, values.KindLesson, "l1"), agentRef(t, values.KindAssignment, "a1"))
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

// evalAsk asks in the eval scope with upload u1 attached.
func evalAsk(t *testing.T, e *agentEnv, text string) agentturn.Step {
	t.Helper()
	st, err := e.turns.Ask(context.Background(), agentturn.AskInput{
		Scope: evalScope(t), Text: text, Uploads: []values.Ref{agentRef(t, values.KindUpload, "u1")},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	return st
}

// okOutcome is a plain successful result of a call, optionally showing the planner some IDs.
func okOutcome(c agentturn.Call, seen ...values.Ref) agentturn.Outcome {
	return agentturn.Outcome{Result: values.ToolResult{CallID: c.Call.ID(), ResultJSON: `{"ok":true}`}, Seen: seen}
}

// play answers whatever the request waits for (results, «Да», answers) until it ends, and returns every card shown.
func play(t *testing.T, e *agentEnv, st agentturn.Step) (agentturn.Step, []values.Card) {
	t.Helper()
	ctx := context.Background()
	cards := append([]values.Card(nil), st.Cards...)
	for range 16 {
		var err error
		switch {
		case len(st.Calls) > 0:
			outs := make([]agentturn.Outcome, 0, len(st.Calls))
			for _, c := range st.Calls {
				outs = append(outs, okOutcome(c))
			}
			st = e.submit(t, st, outs...)
		case st.Status == thread.StatusAwaitingPlan:
			st, err = e.turns.ApprovePlan(ctx, turnOf(st))
		case st.Status == thread.StatusAwaitingAnswers:
			st, err = e.turns.AnswerQuestions(ctx, turnOf(st), []values.Answer{{Question: 0, Option: 0}})
		default:
			return st, cards
		}
		if err != nil {
			t.Fatalf("play: %v", err)
		}
		cards = append(cards, st.Cards...)
	}
	t.Fatalf("play: the request did not end: %+v", st)
	return st, nil
}

// resultOf finds the result the planner got for a call in any request the Hub saw.
func resultOf(e *agentEnv, callID string) (*hubv1.ToolResult, bool) {
	for _, req := range e.hub.Requests() {
		for _, turn := range req.GetTurns() {
			for _, r := range turn.GetResults().GetResults() {
				if r.GetCallId() == callID {
					return r, true
				}
			}
		}
	}
	return nil, false
}

// refusalOf returns the refusal enum of an is_error result.
func refusalOf(t *testing.T, r *hubv1.ToolResult) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if !r.GetIsError() || json.Unmarshal([]byte(r.GetResultJson()), &body) != nil {
		return ""
	}
	return body.Error
}

func cardKinds(cards []values.Card) []values.CardKind {
	var out []values.CardKind
	for _, c := range cards {
		if r, ok := c.(values.ReplyCard); ok && string(r.Text) == evalEndText {
			continue
		}
		out = append(out, c.Kind())
	}
	return out
}

// TestAgentEvalReplayRecordedSteps runs recorded Hub answers (shared/testdata/agent/replay) through the real Hub
// client, the strict argument parser, the policy and the thread, and checks what reaches the Driver, the panel and the
// planner. The Hub side of the same fixtures is hub/tests/agent_eval_test.go.
func TestAgentEvalReplayRecordedSteps(t *testing.T) {
	for _, fx := range loadReplayFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			e := newEvalEnv(t, evalFacts{cost: evalCost})
			calls := make([]testenv.AgentCall, 0, len(fx.Calls))
			for _, c := range fx.Calls {
				calls = append(calls, testenv.AgentCall{ID: c.CallID, Tool: c.Tool, Args: c.ArgumentsJSON})
			}
			e.hub.Then(calls...).Then(evalEnd)

			first := evalAsk(t, e, "проверка записанного ответа")
			if fx.DesktopStop != "" {
				if first.Status != thread.StatusStopped || string(first.StopReason) != fx.DesktopStop || len(first.Calls) != 0 ||
					len(cardKinds(first.Cards)) != 1 || first.Cards[0].Kind() != values.CardStopped {
					t.Fatalf("step = %+v, want stopped (%s) with nothing to run", first, fx.DesktopStop)
				}
				return
			}
			_, cards := play(t, e, first)
			kinds := cardKinds(cards)

			for id, want := range fx.Desktop {
				what, arg, _ := strings.Cut(want, ":")
				switch what {
				case "driver":
					i := indexOfCall(first.Calls, id)
					if i < 0 || string(first.Calls[i].Class) != arg {
						t.Errorf("%s: want it handed to the Driver as %s; calls %+v", id, arg, first.Calls)
					}
				case "card":
					if !containsKind(kinds, values.CardKind(arg)) {
						t.Errorf("%s: want a %s card; cards %v", id, arg, kinds)
					}
					if indexOfCall(first.Calls, id) >= 0 {
						t.Errorf("%s: a card tool reached the Driver", id)
					}
				case "refused":
					r, ok := resultOf(e, id)
					if !ok || refusalOf(t, r) != arg {
						t.Errorf("%s: want refusal %s, the planner got %v", id, arg, r)
					}
					if indexOfCall(first.Calls, id) >= 0 {
						t.Errorf("%s: a refused call reached the Driver", id)
					}
				default:
					t.Fatalf("%s: unknown expectation %q", id, want)
				}
			}
			// Nothing the fixture does not name may run.
			for _, c := range first.Calls {
				if _, ok := fx.Desktop[c.Call.ID()]; !ok {
					t.Errorf("call %s reached the Driver without an expectation", c.Call.ID())
				}
			}
		})
	}
}

func indexOfCall(calls []agentturn.Call, id string) int {
	for i, c := range calls {
		if c.Call.ID() == id {
			return i
		}
	}
	return -1
}

func containsKind(kinds []values.CardKind, k values.CardKind) bool {
	for _, got := range kinds {
		if got == k {
			return true
		}
	}
	return false
}

// TestAgentEvalCToolIsNeverExecuted: a C call reaches the Driver only as class C, which the agent may not execute (a
// proposal card); a write that would replace a ready result is C too; and the agent use cases expose no method that
// confirms, executes, sends or deletes. That the Driver turns class C into a card only is checked with TASK-117.
func TestAgentEvalCToolIsNeverExecuted(t *testing.T) {
	e := newEvalEnv(t, evalFacts{cost: evalCost, replaces: map[values.Tool]bool{values.ToolMakeSummary: true}})
	e.hub.Then(
		testenv.AgentCall{ID: "toolu_1", Tool: "propose_deadline", Args: `{"assignment":"a1"}`},
		testenv.AgentCall{ID: "toolu_2", Tool: "find_lessons", Args: `{"subject":"s1"}`},
	).Then(testenv.AgentCall{ID: "toolu_3", Tool: "make_summary", Args: `{"recording":"rec1"}`}).Then(evalEnd)

	st := evalAsk(t, e, "поставь срок и перескажи лекцию заново")
	if len(st.Calls) != 2 || st.Calls[0].Class != actionpolicy.ClassC || st.Calls[0].Class.AgentCanExecute() ||
		st.Calls[1].Class != actionpolicy.ClassR {
		t.Fatalf("first step calls = %+v", st.Calls)
	}
	st = e.submit(t, st, okOutcome(st.Calls[0]), okOutcome(st.Calls[1], agentRef(t, values.KindRecording, "rec1")))
	if len(st.Calls) != 1 || st.Calls[0].Call.Tool() != values.ToolMakeSummary || st.Calls[0].Class != actionpolicy.ClassC {
		t.Fatalf("a summary that replaces a ready one = %+v, want class C", st.Calls)
	}

	forbidden := []string{"Confirm", "Execute", "Send", "Delete"}
	for _, typ := range []reflect.Type{reflect.TypeOf((*agentturn.Service)(nil)), reflect.TypeOf((*agentjournal.Service)(nil))} {
		for i := range typ.NumMethod() {
			name := typ.Method(i).Name
			for _, f := range forbidden {
				if strings.Contains(name, f) {
					t.Errorf("%s has method %s: the agent must have no path to an irreversible action", typ, name)
				}
			}
		}
	}
}

// TestAgentEvalProvenance: an ID passes only if it was shown to this thread in a field of its kind; IDs the student
// typed, IDs of another kind and IDs seen in another subject's thread are refused.
func TestAgentEvalProvenance(t *testing.T) {
	e := newEvalEnv(t, evalFacts{cost: evalCost})
	// toolu_1 names an ID the student typed, toolu_2 an assignment as a subject, toolu_3 a job never shown, toolu_4 the
	// scope's subject; toolu_5 the job toolu_4's result showed.
	e.hub.Then(
		testenv.AgentCall{ID: "toolu_1", Tool: "delivery_status", Args: `{"assignment":"a77"}`},
		testenv.AgentCall{ID: "toolu_2", Tool: "find_lessons", Args: `{"subject":"a1"}`},
		testenv.AgentCall{ID: "toolu_3", Tool: "job_status", Args: `{"ids":["job-1"]}`},
		testenv.AgentCall{ID: "toolu_4", Tool: "list_assignments", Args: `{"subject":"s1"}`},
	).Then(testenv.AgentCall{ID: "toolu_5", Tool: "job_status", Args: `{"ids":["job-1"]}`}).Then(evalEnd)

	st := evalAsk(t, e, "что с заданием a77?")
	if len(st.Calls) != 1 || st.Calls[0].Call.ID() != "toolu_4" {
		t.Fatalf("first step calls = %+v", st.Calls)
	}
	st = e.submit(t, st, okOutcome(st.Calls[0], agentRef(t, values.KindJob, "job-1")))
	if len(st.Calls) != 1 || st.Calls[0].Call.ID() != "toolu_5" {
		t.Fatalf("an ID shown in a result was not admitted: %+v", st.Calls)
	}
	for _, id := range []string{"toolu_1", "toolu_2", "toolu_3"} {
		if r, ok := resultOf(e, id); !ok || refusalOf(t, r) != string(values.RefuseUnknownID) {
			t.Errorf("%s: result %v, want unknown_id", id, r)
		}
	}
	play(t, e, st)

	// The thread of no subject never saw job-1.
	e.hub.Then(testenv.AgentCall{ID: "toolu_6", Tool: "job_status", Args: `{"ids":["job-1"]}`}).Then(evalEnd)
	other, err := e.turns.Ask(context.Background(), agentturn.AskInput{Text: "что с задачей?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Calls) != 0 || other.Thread == st.Thread {
		t.Fatalf("another thread = %+v", other)
	}
	if r, ok := resultOf(e, "toolu_6"); !ok || refusalOf(t, r) != string(values.RefuseUnknownID) {
		t.Fatalf("an ID of another thread: %v", r)
	}
}

// desktopPlan is a show_plan call in the plan step shape the Hub checks ("arguments").
func desktopPlan(summary string, steps ...string) string {
	return `{"summary":"` + summary + `","steps":[` + strings.Join(steps, ",") + `]}`
}

func desktopStep(tool, args string) string { return `{"tool":"` + tool + `","arguments":` + args + `}` }

// TestAgentEvalDeferredPlanIDs: a deferred plan argument binds only to an ID an earlier approved step created in this
// request; a known ID that the plan did not create is not in the plan.
func TestAgentEvalDeferredPlanIDs(t *testing.T) {
	e := newEvalEnv(t, evalFacts{cost: evalCost})
	plan := desktopPlan("План: папка и файл.",
		desktopStep("create_folder", `{"subject":"s1","name":"Лабы"}`),
		desktopStep("place_upload", `{"upload":"u1","subject":"s1"}`))
	e.hub.Then(testenv.AgentCall{ID: "toolu_1", Tool: "list_subjects", Args: `{}`}).
		Then(testenv.AgentCall{ID: "toolu_2", Tool: "show_plan", Args: plan}).
		Then(testenv.AgentCall{ID: "toolu_3", Tool: "create_folder", Args: `{"subject":"s1","name":"Лабы"}`}).
		Then(testenv.AgentCall{ID: "toolu_4", Tool: "place_upload", Args: `{"upload":"u1","subject":"s1","folder":"f0"}`}).
		Then(testenv.AgentCall{ID: "toolu_5", Tool: "place_upload", Args: `{"upload":"u1","subject":"s1","folder":"f1"}`}).
		Then(evalEnd)
	ctx := context.Background()

	st := evalAsk(t, e, "разложи файл в новую папку Лабы")
	st = e.submit(t, st, okOutcome(st.Calls[0], agentRef(t, values.KindFolder, "f0"))) // f0 is known, not created
	if st.Status != thread.StatusAwaitingPlan {
		t.Fatalf("plan step = %+v", st)
	}
	st, err := e.turns.ApprovePlan(ctx, turnOf(st))
	if err != nil || len(st.Calls) != 1 || st.Calls[0].PlanStep != 0 {
		t.Fatalf("approved create = %+v, %v", st, err)
	}
	created := okOutcome(st.Calls[0])
	created.Created = agentRef(t, values.KindFolder, "f1")
	st = e.submit(t, st, created)
	if len(st.Calls) != 1 || st.Calls[0].Call.ID() != "toolu_5" || st.Calls[0].PlanStep != 1 {
		t.Fatalf("after the create = %+v, want only the call with the created folder", st.Calls)
	}
	if r, ok := resultOf(e, "toolu_4"); !ok || refusalOf(t, r) != string(values.RefuseNotInPlan) {
		t.Fatalf("a known but not created folder: %v", r)
	}
	if st, _ = play(t, e, st); st.Status != thread.StatusDone {
		t.Fatalf("end = %+v", st)
	}
}

// TestAgentEvalThirdQuestionRound: the third ask_questions is refused with a note to go on with stated assumptions;
// the request is not stopped and the student sees no third card.
func TestAgentEvalThirdQuestionRound(t *testing.T) {
	e := newEvalEnv(t, evalFacts{cost: evalCost})
	q := `{"questions":[{"kind":"text","question":"Какой объём?"}]}`
	e.hub.Then(testenv.AgentCall{ID: "q1", Tool: "ask_questions", Args: q}).
		Then(testenv.AgentCall{ID: "q2", Tool: "ask_questions", Args: q}).
		Then(testenv.AgentCall{ID: "q3", Tool: "ask_questions", Args: q}).
		Then(evalEnd)

	st, cards := play(t, e, evalAsk(t, e, "напиши реферат"))
	if st.Status != thread.StatusDone || st.StopReason != "" {
		t.Fatalf("end = %+v, want done without a stop", st)
	}
	rounds := 0
	for _, c := range cards {
		if qc, ok := c.(values.QuestionsCard); ok {
			rounds++
			if qc.Round > 2 {
				t.Fatalf("question card of round %d", qc.Round)
			}
		}
	}
	r, ok := resultOf(e, "q3")
	if rounds != 2 || !ok || refusalOf(t, r) != string(values.RefuseTooManyRounds) ||
		!strings.Contains(r.GetResultJson(), "continue_with_stated_assumptions") {
		t.Fatalf("rounds %d, third round result %v", rounds, r)
	}
}

// TestAgentEvalBudget: paid reads do not count as the paid job without a plan, a second paid job needs a plan, a paid
// job of unknown cost needs a plan, and the step, retry and plan size limits stop or refuse.
func TestAgentEvalBudget(t *testing.T) {
	ctx := context.Background()

	t.Run("paid reads and one paid job", func(t *testing.T) {
		e := newEvalEnv(t, evalFacts{cost: evalCost})
		e.hub.Then(
			testenv.AgentCall{ID: "r1", Tool: "inspect_upload", Args: `{"upload":"u1"}`},
			testenv.AgentCall{ID: "r2", Tool: "answer_question", Args: `{"question":"Что такое ряд?"}`},
			testenv.AgentCall{ID: "w1", Tool: "triage_assignment", Args: `{"upload":"u1"}`},
		).Then(testenv.AgentCall{ID: "w2", Tool: "triage_assignment", Args: `{"assignment":"a1"}`}).Then(evalEnd)
		st := evalAsk(t, e, "разбери задание из файла")
		if len(st.Calls) != 3 || !st.Calls[0].Paid || !st.Calls[1].Paid || st.Calls[2].Class != actionpolicy.ClassU {
			t.Fatalf("calls = %+v", st.Calls)
		}
		play(t, e, st)
		if r, ok := resultOf(e, "w2"); !ok || refusalOf(t, r) != string(values.RefusePlanRequired) {
			t.Fatalf("second paid job: %v", r)
		}
	})

	t.Run("unknown cost needs a plan", func(t *testing.T) {
		e := newEvalEnv(t, zeroFacts{})
		e.hub.Then(testenv.AgentCall{ID: "w1", Tool: "triage_assignment", Args: `{"upload":"u1"}`}).Then(evalEnd)
		st := evalAsk(t, e, "разбери задание")
		if len(st.Calls) != 0 {
			t.Fatalf("a paid job of unknown cost ran without a plan: %+v", st.Calls)
		}
		if r, ok := resultOf(e, "w1"); !ok || refusalOf(t, r) != string(values.RefusePlanRequired) {
			t.Fatalf("result %v", r)
		}
	})

	t.Run("steps", func(t *testing.T) {
		e := newEvalEnv(t, evalFacts{cost: evalCost})
		for i := range thread.MaxSteps + 1 {
			e.hub.Then(testenv.AgentCall{ID: fmt.Sprintf("toolu_%d", i), Tool: "list_subjects", Args: `{}`})
		}
		st := evalAsk(t, e, "пройдись по всем предметам")
		for len(st.Calls) > 0 {
			st = e.submit(t, st, okOutcome(st.Calls[0]))
		}
		if st.Status != thread.StatusStopped || st.StopReason != values.StopBudget || len(e.hub.Requests()) != thread.MaxSteps {
			t.Fatalf("after %d Hub steps: %+v", len(e.hub.Requests()), st)
		}
		if _, err := e.turns.Continue(ctx, turnOf(st)); !errors.Is(err, thread.ErrBudgetExceeded) {
			t.Fatalf("Continue after a budget stop: %v", err)
		}
	})

	t.Run("retries", func(t *testing.T) {
		e := newEvalEnv(t, evalFacts{cost: evalCost})
		for range thread.MaxRetries + 1 {
			e.hub.ThenProviderFails()
		}
		st := evalAsk(t, e, "привет")
		for i := range thread.MaxRetries {
			var err error
			if st, err = e.turns.Retry(ctx, turnOf(st)); err != nil || st.StopReason != values.StopFailed {
				t.Fatalf("retry %d = %+v, %v", i+1, st, err)
			}
		}
		st, err := e.turns.Retry(ctx, turnOf(st))
		if !errors.Is(err, thread.ErrBudgetExceeded) || st.StopReason != values.StopBudget {
			t.Fatalf("retry over the cap = %+v, %v", st, err)
		}
		if n := len(e.hub.Requests()); n != thread.MaxRetries+1 {
			t.Fatalf("Hub calls = %d", n)
		}
	})

	t.Run("plan with too many writes", func(t *testing.T) {
		e := newEvalEnv(t, evalFacts{cost: evalCost})
		var steps []string
		for i := range thread.MaxWrites + 1 {
			steps = append(steps, desktopStep("create_subject", fmt.Sprintf(`{"name":"Предмет %c"}`, 'А'+rune(i))))
		}
		e.hub.Then(testenv.AgentCall{ID: "p1", Tool: "show_plan", Args: desktopPlan("План: много предметов.", steps...)}).Then(evalEnd)
		st, _ := play(t, e, evalAsk(t, e, "заведи семь предметов"))
		if r, ok := resultOf(e, "p1"); st.Status != thread.StatusDone || !ok || refusalOf(t, r) != string(values.RefuseInvalidPlan) {
			t.Fatalf("plan of %d writes: %v, %+v", thread.MaxWrites+1, r, st)
		}
	})
}
