package thread

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
)

var t0 = time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func ref(t *testing.T, kind values.IDKind, id string) values.Ref {
	t.Helper()
	r, err := values.NewRef(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func call(t *testing.T, tool, args string) values.Call {
	t.Helper()
	c, err := values.ParseCall("c1", tool, args)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func planSteps(t *testing.T, steps string) []values.PlanStep {
	t.Helper()
	return call(t, "show_plan", `{"summary":"План","steps":`+steps+`}`).Args().PlanSteps("steps")
}

func subjectName(i int) string { return "Предмет " + string(rune('A'+i)) }

// createSubjects returns a plan of n create_subject steps with names subjectName(0..n-1), then the extra steps.
func createSubjects(n int, extra string) string {
	s := `[`
	for i := range n {
		if i > 0 {
			s += `,`
		}
		s += `{"tool":"create_subject","arguments":{"name":"` + subjectName(i) + `"}}`
	}
	if extra != "" {
		s += `,` + extra
	}
	return s + `]`
}

// context is the base and scope of the tests: subject s1, lesson l1, recording r1, upload u1.
func context(t *testing.T) values.RefSet {
	return values.NewRefSet(ref(t, values.KindSubject, "s1"), ref(t, values.KindLesson, "l1"),
		ref(t, values.KindRecording, "r1"), ref(t, values.KindUpload, "u1"))
}

func newThread(t *testing.T) *Thread {
	t.Helper()
	th, err := New("th1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.StartTurn(at(1)); err != nil {
		t.Fatal(err)
	}
	return th
}

func status(t *testing.T, th *Thread) Status {
	t.Helper()
	turn, ok := th.Turn()
	if !ok {
		t.Fatal("no turn")
	}
	return turn.Status()
}

// reach brings a new thread's request to the given status through its methods.
func reach(t *testing.T, s Status) *Thread {
	t.Helper()
	th := newThread(t)
	var err error
	switch s {
	case StatusPlanning:
	case StatusAwaitingPlan:
		err = th.ProposePlan(planSteps(t, `[{"tool":"make_summary","arguments":{"recording":"r1"}}]`), context(t), at(2))
	case StatusAwaitingAnswers:
		_, err = th.AskQuestions(at(2))
	case StatusExecuting:
		_, err = th.Admit(call(t, "create_subject", `{"name":"Физика"}`), Facts{}, at(2))
	case StatusDone:
		err = th.Complete(at(2))
	case StatusStopped:
		err = th.Stop(values.StopUser, at(2))
	case StatusFailed:
		err = th.Fail(at(2))
	case StatusCancelled:
		err = th.Cancel(at(2))
	}
	if err != nil {
		t.Fatalf("reach %s: %v", s, err)
	}
	if got := status(t, th); got != s {
		t.Fatalf("reached %s, want %s", got, s)
	}
	return th
}

func TestTransitionTable(t *testing.T) {
	want := map[Status][]Status{
		StatusPlanning:        {StatusAwaitingPlan, StatusAwaitingAnswers, StatusExecuting, StatusDone, StatusStopped, StatusFailed, StatusCancelled},
		StatusAwaitingPlan:    {StatusExecuting, StatusPlanning, StatusCancelled},
		StatusAwaitingAnswers: {StatusPlanning, StatusCancelled},
		StatusExecuting:       {StatusAwaitingPlan, StatusDone, StatusStopped, StatusFailed, StatusCancelled},
		StatusStopped:         {StatusPlanning, StatusExecuting, StatusCancelled},
	}
	if len(transitions) != len(Statuses()) {
		t.Fatalf("table has %d statuses, Statuses() %d", len(transitions), len(Statuses()))
	}
	for _, from := range Statuses() {
		allowed := map[Status]bool{}
		for _, to := range want[from] {
			allowed[to] = true
		}
		for _, to := range Statuses() {
			if got := CanTransition(from, to); got != allowed[to] {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, allowed[to])
			}
		}
		if from.IsTerminal() != (len(want[from]) == 0) {
			t.Errorf("%s.IsTerminal() = %v", from, from.IsTerminal())
		}
	}
	if Status("RUNNING").Valid() {
		t.Error("unknown status is valid")
	}
}

// TestOperationsFromEveryStatus runs every operation from every status: it either moves the request to the status
// the table allows or is refused without a change (ARCH-62).
func TestOperationsFromEveryStatus(t *testing.T) {
	type op func(t *testing.T, th *Thread) error
	ops := map[string]op{
		"ProposePlan": func(t *testing.T, th *Thread) error {
			return th.ProposePlan(planSteps(t, `[{"tool":"make_summary","arguments":{"recording":"r1"}}]`), context(t), at(5))
		},
		"ApprovePlan":     func(_ *testing.T, th *Thread) error { return th.ApprovePlan(at(5)) },
		"RevisePlan":      func(_ *testing.T, th *Thread) error { return th.RevisePlan(at(5)) },
		"AskQuestions":    func(_ *testing.T, th *Thread) error { _, err := th.AskQuestions(at(5)); return err },
		"AnswerQuestions": func(_ *testing.T, th *Thread) error { return th.AnswerQuestions(at(5)) },
		"AdmitRead": func(t *testing.T, th *Thread) error {
			_, err := th.Admit(call(t, "list_subjects", `{}`), Facts{}, at(5))
			return err
		},
		"Complete": func(_ *testing.T, th *Thread) error { return th.Complete(at(5)) },
		"Fail":     func(_ *testing.T, th *Thread) error { return th.Fail(at(5)) },
		"Stop":     func(_ *testing.T, th *Thread) error { return th.Stop(values.StopOffline, at(5)) },
		"Continue": func(_ *testing.T, th *Thread) error { return th.Continue(at(5)) },
		"Cancel":   func(_ *testing.T, th *Thread) error { return th.Cancel(at(5)) },
		"BeginStep": func(_ *testing.T, th *Thread) error {
			_, err := th.BeginStep(at(5))
			return err
		},
	}
	// want[op][from] is the status after a successful op; a missing entry means the op is refused.
	want := map[string]map[Status]Status{
		"ProposePlan":     {StatusPlanning: StatusAwaitingPlan, StatusExecuting: StatusAwaitingPlan},
		"ApprovePlan":     {StatusAwaitingPlan: StatusExecuting},
		"RevisePlan":      {StatusAwaitingPlan: StatusPlanning},
		"AskQuestions":    {StatusPlanning: StatusAwaitingAnswers},
		"AnswerQuestions": {StatusAwaitingAnswers: StatusPlanning},
		"AdmitRead":       {StatusPlanning: StatusPlanning, StatusExecuting: StatusExecuting},
		"Complete":        {StatusPlanning: StatusDone, StatusExecuting: StatusDone},
		"Fail":            {StatusPlanning: StatusFailed, StatusExecuting: StatusFailed},
		"Stop":            {StatusPlanning: StatusStopped, StatusExecuting: StatusStopped},
		"Continue":        {StatusStopped: StatusPlanning},
		"Cancel": {
			StatusPlanning: StatusCancelled, StatusAwaitingPlan: StatusCancelled, StatusAwaitingAnswers: StatusCancelled,
			StatusExecuting: StatusCancelled, StatusStopped: StatusCancelled,
		},
		"BeginStep": {StatusPlanning: StatusPlanning, StatusExecuting: StatusExecuting},
	}
	for name, run := range ops {
		for _, from := range Statuses() {
			t.Run(name+"/"+string(from), func(t *testing.T) {
				th := reach(t, from)
				before := th.State()
				err := run(t, th)
				to, ok := want[name][from]
				if !ok {
					if !errors.Is(err, ErrInvalidTransition) {
						t.Fatalf("error = %v, want ErrInvalidTransition", err)
					}
					if got := status(t, th); got != from || th.State().UpdatedAt != before.UpdatedAt {
						t.Fatalf("refused op changed the request: %s", got)
					}
					return
				}
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if got := status(t, th); got != to {
					t.Fatalf("status = %s, want %s", got, to)
				}
			})
		}
	}
}

func TestStartTurn(t *testing.T) {
	tests := []struct {
		from    Status
		wantErr error
	}{
		{StatusPlanning, ErrTurnInProgress},
		{StatusExecuting, ErrTurnInProgress},
		{StatusAwaitingPlan, nil},
		{StatusAwaitingAnswers, nil},
		{StatusStopped, nil},
		{StatusDone, nil},
		{StatusFailed, nil},
		{StatusCancelled, nil},
	}
	for _, tt := range tests {
		t.Run(string(tt.from), func(t *testing.T) {
			th := reach(t, tt.from)
			err := th.StartTurn(at(9))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("StartTurn error = %v, want %v", err, tt.wantErr)
			}
			turn, _ := th.Turn()
			if tt.wantErr != nil {
				if turn.Number() != 1 {
					t.Fatal("refused StartTurn changed the request")
				}
				return
			}
			if turn.Number() != 2 || turn.Status() != StatusPlanning || turn.Budget() != (Budget{}) {
				t.Fatalf("new request = %d %s %+v", turn.Number(), turn.Status(), turn.Budget())
			}
		})
	}

	empty, err := New("th2", t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.Complete(at(1)); !errors.Is(err, ErrNoTurn) {
		t.Fatalf("op without a request = %v", err)
	}
	if _, ok := empty.Turn(); ok {
		t.Fatal("empty thread has a request")
	}
}

func TestStepsAndKeys(t *testing.T) {
	th := newThread(t)
	key, err := th.BeginStep(at(2))
	if err != nil || key != "ag:th1:1:1" {
		t.Fatalf("BeginStep = %q, %v", key, err)
	}
	if _, err := th.BeginStep(at(2)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second open step = %v", err)
	}
	if _, err := th.Admit(call(t, "list_subjects", `{}`), Facts{}, at(2)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Admit during an open step = %v", err)
	}

	// Offline: the request stops with the step open, «Продолжить» repeats the same key.
	if err := th.Stop(values.StopOffline, at(3)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.StopReason() != values.StopOffline || !turn.StepOpen() {
		t.Fatalf("stopped request = %+v", turn)
	}
	if err := th.Continue(at(4)); err != nil {
		t.Fatal(err)
	}
	if again, err := th.StepKey(); err != nil || again != key {
		t.Fatalf("StepKey after Continue = %q, %v", again, err)
	}
	if turn, _ := th.Turn(); turn.StopReason() != "" {
		t.Fatal("stop reason kept after Continue")
	}

	if err := th.EndStep(0, 10, at(5)); !errors.Is(err, ErrInvalidThread) {
		t.Fatalf("EndStep without calls = %v", err)
	}
	if err := th.EndStep(2, 1000, at(5)); err != nil {
		t.Fatal(err)
	}
	if _, err := th.StepKey(); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("StepKey without an open step = %v", err)
	}
	if err := th.EndStep(1, 1, at(5)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("EndStep twice = %v", err)
	}
	next, err := th.BeginStep(at(6))
	if err != nil || next != "ag:th1:1:2" {
		t.Fatalf("next key = %q, %v", next, err)
	}
	if turn, _ := th.Turn(); turn.Budget().InputTokens != 1000 || turn.Budget().Steps != 2 {
		t.Fatalf("budget = %+v", turn.Budget())
	}
}

func assertBudgetStop(t *testing.T, th *Thread, err error) {
	t.Helper()
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("error = %v, want ErrBudgetExceeded", err)
	}
	turn, _ := th.Turn()
	if turn.Status() != StatusStopped || turn.StopReason() != values.StopBudget {
		t.Fatalf("request = %s %s, want STOPPED budget", turn.Status(), turn.StopReason())
	}
	if err := th.Continue(at(30)); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("Continue after a budget stop = %v", err)
	}
	if err := th.Cancel(at(31)); err != nil {
		t.Fatalf("Cancel after a budget stop = %v", err)
	}
}

func TestBudget(t *testing.T) {
	t.Run("steps", func(t *testing.T) {
		th := newThread(t)
		for i := range MaxSteps {
			if _, err := th.BeginStep(at(i)); err != nil {
				t.Fatalf("step %d: %v", i+1, err)
			}
			if err := th.EndStep(1, 10, at(i)); err != nil {
				t.Fatal(err)
			}
		}
		_, err := th.BeginStep(at(20))
		assertBudgetStop(t, th, err)
	})
	t.Run("calls per step", func(t *testing.T) {
		th := newThread(t)
		if _, err := th.BeginStep(at(2)); err != nil {
			t.Fatal(err)
		}
		assertBudgetStop(t, th, th.EndStep(MaxCallsPerStep+1, 10, at(3)))
	})
	t.Run("input tokens", func(t *testing.T) {
		th := newThread(t)
		if _, err := th.BeginStep(at(2)); err != nil {
			t.Fatal(err)
		}
		if err := th.EndStep(MaxCallsPerStep, MaxInputTokens, at(3)); err != nil {
			t.Fatal(err)
		}
		_, err := th.BeginStep(at(4))
		assertBudgetStop(t, th, err)
	})
	// Beta 2026-10-28: every planner step resends a ~13k-token prefix (catalog, history window), so a free-topic
	// request (list_subjects, triage, questions, write_work, reply...) must fit all MaxSteps on input tokens.
	t.Run("full request of 13k steps fits", func(t *testing.T) {
		th := newThread(t)
		for i := range MaxSteps {
			if _, err := th.BeginStep(at(i)); err != nil {
				t.Fatalf("step %d: %v", i+1, err)
			}
			if err := th.EndStep(1, 13_300, at(i)); err != nil {
				t.Fatal(err)
			}
		}
		_, err := th.BeginStep(at(20))
		assertBudgetStop(t, th, err)
		if turn, _ := th.Turn(); turn.Budget().Steps != MaxSteps {
			t.Fatalf("stopped after %d steps, want the step limit", turn.Budget().Steps)
		}
	})
	t.Run("runaway context stops before the step limit", func(t *testing.T) {
		th := newThread(t)
		steps := 0
		var err error
		for ; steps < MaxSteps; steps++ {
			if _, err = th.BeginStep(at(steps)); err != nil {
				break
			}
			if err := th.EndStep(1, 3*MaxStepInputTokens, at(steps)); err != nil {
				t.Fatal(err)
			}
		}
		assertBudgetStop(t, th, err)
		if steps >= MaxSteps/2 {
			t.Fatalf("a runaway ran %d steps", steps)
		}
	})
	t.Run("writes", func(t *testing.T) {
		th := newThread(t)
		if err := th.ProposePlan(planSteps(t, createSubjects(MaxWrites, "")), nil, at(2)); err != nil {
			t.Fatal(err)
		}
		if err := th.ApprovePlan(at(3)); err != nil {
			t.Fatal(err)
		}
		for i := range MaxWrites {
			if _, err := th.Admit(call(t, "create_subject", `{"name":"`+subjectName(i)+`"}`), Facts{}, at(4)); err != nil {
				t.Fatalf("write %d: %v", i+1, err)
			}
		}
		_, err := th.Admit(call(t, "create_subject", `{"name":"Ещё"}`), Facts{}, at(5))
		assertBudgetStop(t, th, err)
	})
	t.Run("question rounds", func(t *testing.T) {
		th := newThread(t)
		for round := 1; round <= values.MaxQuestionRounds; round++ {
			got, err := th.AskQuestions(at(round))
			if err != nil || got != round {
				t.Fatalf("round %d: %d, %v", round, got, err)
			}
			if err := th.AnswerQuestions(at(round)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := th.AskQuestions(at(9)); !errors.Is(err, ErrTooManyRounds) || !IsRefusal(err) {
			t.Fatalf("third round = %v", err)
		}
		if status(t, th) != StatusPlanning {
			t.Fatal("refused round changed the status")
		}
	})
	t.Run("paid job without a plan", func(t *testing.T) {
		th := newThread(t)
		s := th.State()
		s.Version = 1
		s.Current.Budget.PaidUnplanned = MaxPaidUnplanned
		th, err := Restore(s)
		if err != nil {
			t.Fatal(err)
		}
		_, err = th.Admit(call(t, "make_summary", `{"recording":"r1"}`), Facts{Context: context(t), CostMicroUSD: 1000}, at(3))
		if !errors.Is(err, ErrPlanRequired) {
			t.Fatalf("second paid job without a plan = %v", err)
		}
	})
}

func TestAdmitWithoutPlan(t *testing.T) {
	ctx := context(t)
	tests := []struct {
		name      string
		tool      string
		args      string
		facts     Facts
		wantErr   error
		wantClass actionpolicy.Class
		writes    int
	}{
		{"read", "list_subjects", `{}`, Facts{}, nil, actionpolicy.ClassR, 0},
		{"free write", "set_lesson_info", `{"recording":"r1","subject":"s1","kind":"lecture"}`, Facts{Context: ctx}, nil, actionpolicy.ClassU, 1},
		{"cheap paid write", "make_summary", `{"recording":"r1"}`, Facts{Context: ctx, CostMicroUSD: 1000}, nil, actionpolicy.ClassU, 1},
		{"costly paid write", "make_summary", `{"recording":"r1"}`, Facts{Context: ctx, CostMicroUSD: 90_000}, ErrPlanRequired, "", 0},
		{"lecture text", "make_lecture_text", `{"recording":"r1"}`, Facts{Context: ctx, CostMicroUSD: 1}, ErrPlanRequired, "", 0},
		{"after partial triage", "set_lesson_info", `{"recording":"r1","subject":"s1","kind":"lab"}`, Facts{Context: ctx, Triage: actionpolicy.TriagePartly}, ErrPlanRequired, "", 0},
		{"replaces a ready result", "make_summary", `{"recording":"r1"}`, Facts{Context: ctx, CostMicroUSD: 1, ReplacesReady: true}, ErrPlanRequired, "", 0},
		{"C proposal", "propose_delete_recording", `{"recording":"r1"}`, Facts{Context: ctx}, nil, actionpolicy.ClassC, 0},
		{"UI", "reply", `{"text":"Готово"}`, Facts{}, nil, actionpolicy.ClassUI, 0},
		{"unknown id", "set_lesson_info", `{"recording":"r2","subject":"s1","kind":"lab"}`, Facts{Context: ctx}, actionpolicy.ErrUnknownID, "", 0},
		{"lesson id as recording", "make_summary", `{"recording":"l1"}`, Facts{Context: ctx, CostMicroUSD: 1}, actionpolicy.ErrUnknownID, "", 0},
		{"id from nowhere in a C call", "propose_send", `{"assignment":"a1","version":"v1"}`, Facts{Context: ctx}, actionpolicy.ErrUnknownID, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := newThread(t)
			adm, err := th.Admit(call(t, tt.tool, tt.args), tt.facts, at(2))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Admit error = %v, want %v", err, tt.wantErr)
			}
			if err != nil && !IsRefusal(err) {
				t.Fatalf("%v is not a refusal", err)
			}
			if err == nil && (adm.Class != tt.wantClass || adm.PlanStep != -1) {
				t.Fatalf("admission = %+v", adm)
			}
			turn, _ := th.Turn()
			if turn.Budget().Writes != tt.writes {
				t.Fatalf("writes = %d, want %d", turn.Budget().Writes, tt.writes)
			}
			wantStatus := StatusPlanning
			if tt.writes > 0 {
				wantStatus = StatusExecuting
			}
			if turn.Status() != wantStatus {
				t.Fatalf("status = %s, want %s", turn.Status(), wantStatus)
			}
		})
	}

	t.Run("second write needs a plan", func(t *testing.T) {
		th := newThread(t)
		if _, err := th.Admit(call(t, "create_subject", `{"name":"Физика"}`), Facts{}, at(2)); err != nil {
			t.Fatal(err)
		}
		if _, err := th.Admit(call(t, "create_subject", `{"name":"Химия"}`), Facts{}, at(3)); !errors.Is(err, ErrPlanRequired) {
			t.Fatalf("second write = %v", err)
		}
	})
	t.Run("ids from results of this thread", func(t *testing.T) {
		th := newThread(t)
		c := call(t, "link_material", `{"material":"m1","lesson":"l1"}`)
		if _, err := th.Admit(c, Facts{Context: ctx}, at(2)); !errors.Is(err, actionpolicy.ErrUnknownID) {
			t.Fatalf("before Observe = %v", err)
		}
		if err := th.Observe(ref(t, values.KindMaterial, "m1")); err != nil {
			t.Fatal(err)
		}
		if !th.Seen(ref(t, values.KindMaterial, "m1")) || th.Seen(ref(t, values.KindUpload, "m1")) {
			t.Fatal("Seen is not typed")
		}
		if _, err := th.Admit(c, Facts{Context: ctx}, at(3)); err != nil {
			t.Fatalf("after Observe = %v", err)
		}
		if err := th.Observe(values.Ref{}); !errors.Is(err, ErrInvalidThread) {
			t.Fatalf("Observe(zero) = %v", err)
		}
	})
}

func TestApprovedPlan(t *testing.T) {
	ctx := context(t)
	plan := `[{"tool":"place_upload","arguments":{"upload":"u1","subject":"s1","folder":"f1"}},{"tool":"make_lecture_text","arguments":{"recording":"r1"}},{"tool":"propose_delete_recording","arguments":{"recording":"r1"}}]`
	th := newThread(t)
	if err := th.Observe(ref(t, values.KindFolder, "f1")); err != nil {
		t.Fatal(err)
	}
	if err := th.ProposePlan(planSteps(t, plan), ctx, at(2)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); len(turn.ProposedPlan()) != 3 || len(turn.ApprovedPlan()) != 0 {
		t.Fatal("proposed plan not recorded")
	}
	if _, err := th.Admit(call(t, "list_subjects", `{}`), Facts{}, at(2)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Admit while awaiting the plan = %v", err)
	}
	if err := th.ApprovePlan(at(3)); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		name    string
		tool    string
		args    string
		wantErr error
		step    int
	}{
		{"not in plan", "create_subject", `{"name":"Физика"}`, ErrNotInPlan, 0},
		{"other id than planned", "make_lecture_text", `{"recording":"r9"}`, actionpolicy.ErrUnknownID, 0},
		{"planned lecture text", "make_lecture_text", `{"recording":"r1"}`, nil, 1},
		{"planned placement", "place_upload", `{"upload":"u1","subject":"s1","folder":"f1"}`, nil, 0},
		{"each step once", "make_lecture_text", `{"recording":"r1"}`, ErrNotInPlan, 0},
	}
	for _, s := range steps {
		adm, err := th.Admit(call(t, s.tool, s.args), Facts{Context: ctx}, at(4))
		if !errors.Is(err, s.wantErr) {
			t.Fatalf("%s: error = %v, want %v", s.name, err, s.wantErr)
		}
		if err == nil && adm.PlanStep != s.step {
			t.Fatalf("%s: step = %d, want %d", s.name, adm.PlanStep, s.step)
		}
	}
	if turn, _ := th.Turn(); turn.Budget().Writes != 2 || turn.Budget().PaidUnplanned != 0 {
		t.Fatalf("budget = %+v", turn.Budget())
	}

	// A new plan from EXECUTING replaces the approved one once approved.
	if err := th.ProposePlan(planSteps(t, `[{"tool":"create_subject","arguments":{"name":"Химия"}}]`), ctx, at(5)); err != nil {
		t.Fatal(err)
	}
	if err := th.ApprovePlan(at(6)); err != nil {
		t.Fatal(err)
	}
	if _, err := th.Admit(call(t, "create_subject", `{"name":"Химия"}`), Facts{}, at(7)); err != nil {
		t.Fatalf("write of the new plan = %v", err)
	}
}

func TestProposePlanRules(t *testing.T) {
	ctx := context(t)
	tests := []struct {
		name  string
		steps string
		want  error
	}{
		{"read step", `[{"tool":"list_subjects"}]`, ErrInvalidPlan},
		{"unknown id", `[{"tool":"make_summary","arguments":{"recording":"r7"}}]`, actionpolicy.ErrUnknownID},
		{"too many writes", createSubjects(MaxWrites+1, ""), ErrInvalidPlan},
		{"C steps do not count as writes", createSubjects(MaxWrites, `{"tool":"propose_delete_recording","arguments":{"recording":"r1"}}`), nil},
		{"created subject", `[{"tool":"create_subject","arguments":{"name":"Физика"}},{"tool":"create_folder","arguments":{"name":"Лекции"}}]`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := newThread(t)
			err := th.ProposePlan(planSteps(t, tt.steps), ctx, at(2))
			if !errors.Is(err, tt.want) {
				t.Fatalf("ProposePlan error = %v, want %v", err, tt.want)
			}
			if err != nil && status(t, th) != StatusPlanning {
				t.Fatal("refused plan changed the status")
			}
		})
	}
	th := newThread(t)
	if err := th.ProposePlan(nil, ctx, at(2)); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("empty plan = %v", err)
	}
	// A step without its required arguments would approve any call of the tool, so show_plan is refused before a
	// plan reaches ProposePlan; a UI step cannot carry its text arguments at all.
	for _, steps := range []string{
		`[{"tool":"make_lecture_text"}]`,
		`[{"tool":"edit_due","arguments":{"assignment":"a1"}}]`,
		`[{"tool":"reply"}]`,
	} {
		_, err := values.ParseCall("c1", "show_plan", `{"summary":"План","steps":`+steps+`}`)
		if !errors.Is(err, values.ErrInvalidArguments) {
			t.Fatalf("incomplete plan %s = %v", steps, err)
		}
	}
}

func TestRevisePlanDropsApprovedPlan(t *testing.T) {
	ctx := context(t)
	th := newThread(t)
	if err := th.ProposePlan(planSteps(t, `[{"tool":"make_lecture_text","arguments":{"recording":"r1"}}]`), ctx, at(2)); err != nil {
		t.Fatal(err)
	}
	if err := th.ApprovePlan(at(3)); err != nil {
		t.Fatal(err)
	}
	// EXECUTING -> new plan -> AWAITING_PLAN -> «Изменить» -> PLANNING: the old approval is gone.
	if err := th.ProposePlan(planSteps(t, `[{"tool":"create_subject","arguments":{"name":"Химия"}}]`), ctx, at(4)); err != nil {
		t.Fatal(err)
	}
	if err := th.RevisePlan(at(5)); err != nil {
		t.Fatal(err)
	}
	turn, _ := th.Turn()
	if turn.Status() != StatusPlanning || len(turn.ApprovedPlan()) != 0 || len(th.State().Current.Used) != 0 {
		t.Fatalf("after RevisePlan: %s, approved %d", turn.Status(), len(turn.ApprovedPlan()))
	}
	s := th.State()
	s.Version = 1
	if _, err := Restore(s); err != nil {
		t.Fatalf("Restore after RevisePlan = %v", err)
	}
	_, err := th.Admit(call(t, "make_lecture_text", `{"recording":"r1"}`), Facts{Context: ctx, CostMicroUSD: 90_000}, at(6))
	if !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("write of the dropped plan = %v", err)
	}
}

func TestRevisePlanDropsProposal(t *testing.T) {
	th := reach(t, StatusAwaitingPlan)
	if err := th.RevisePlan(at(5)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); len(turn.ProposedPlan()) != 0 {
		t.Fatal("proposal kept after «Изменить»")
	}
}

func TestNewChecksIDAndTime(t *testing.T) {
	if _, err := New("a/b", t0); !errors.Is(err, ErrInvalidThread) {
		t.Fatalf("New with a bad id = %v", err)
	}
	if _, err := New("th1", time.Time{}); !errors.Is(err, ErrInvalidThread) {
		t.Fatalf("New without a time = %v", err)
	}
}

func TestWindowStart(t *testing.T) {
	kb := 1 << 10
	// pairsOf numbers the pairs from first on, one text and one reply each.
	pairsOf := func(first int, sizes ...int) []pair {
		out := make([]pair, len(sizes))
		for i, s := range sizes {
			out[i] = pair{number: first + i, turns: make([]values.HistoryTurn, 2), size: s}
		}
		return out
	}
	many, heavy := make([]int, 25), make([]int, 10)
	for i := range many {
		many[i] = 100
	}
	for i := range heavy {
		heavy[i] = 9 * kb
	}
	tests := []struct {
		name     string
		pairs    []pair
		nowTurns int
		want     int
	}{
		{"none", nil, 1, 0},
		{"all fit", pairsOf(1, 100, 200), 1, 0},
		{"pair limit moves a block", pairsOf(1, many...), 1, 5},
		// 3..27: from 6 there are 22 pairs, from 11 there are 17.
		{"pair limit from a later first request", pairsOf(3, many...), 1, 8},
		{"byte limit moves a block", pairsOf(1, heavy...), 1, 5},
		{"byte limit past the last block start, pair by pair", pairsOf(1, 20*kb, 20*kb, 20*kb, 20*kb), 1, 2},
		{"exactly the byte limit", pairsOf(1, 1, 24*kb, 24*kb), 1, 1},
		{"newest too large", pairsOf(1, 100, 49*kb), 1, 2},
		{"turn limit", pairsOf(1, many[:12]...), 41, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := windowStart(tt.pairs, 0, tt.nowTurns, 0); got != tt.want {
				t.Fatalf("windowStart = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRestore(t *testing.T) {
	th := reach(t, StatusExecuting)
	if err := th.Observe(ref(t, values.KindMaterial, "m1")); err != nil {
		t.Fatal(err)
	}
	if err := th.Stop(values.StopUser, at(3)); err != nil {
		t.Fatal(err)
	}
	good := th.State()
	good.Version = 3

	restored, err := Restore(good)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !restored.Seen(ref(t, values.KindMaterial, "m1")) || restored.Version() != 3 {
		t.Fatal("restored thread lost data")
	}
	if err := restored.Continue(at(4)); err != nil || status(t, restored) != StatusExecuting {
		t.Fatalf("Continue after Restore = %v", err)
	}

	planned := reach(t, StatusAwaitingPlan).State()
	planned.Version = 1
	if _, err := Restore(planned); err != nil {
		t.Fatalf("Restore awaiting plan: %v", err)
	}

	bad := []struct {
		name   string
		mutate func(s *State)
	}{
		{"version 0", func(s *State) { s.Version = 0 }},
		{"bad id", func(s *State) { s.ID = "a/b" }},
		{"turns without current", func(s *State) { s.Current = nil }},
		{"current without turns", func(s *State) { s.Turns = 0 }},
		{"number mismatch", func(s *State) { s.Turns = 2 }},
		{"unknown status", func(s *State) { s.Current.Status = "RUNNING" }},
		{"stopped without resume", func(s *State) { s.Current.Resume = "" }},
		{"resume to a waiting status", func(s *State) { s.Current.Resume = StatusAwaitingPlan }},
		{"bad stop reason", func(s *State) { s.Current.StopReason = "x" }},
		{"stop reason while running", func(s *State) { s.Current.Status, s.Current.Resume = StatusExecuting, "" }},
		{"steps over budget", func(s *State) { s.Current.Budget.Steps = MaxSteps + 1 }},
		{"writes over budget", func(s *State) { s.Current.Budget.Writes = MaxWrites + 1 }},
		{"third round", func(s *State) { s.Current.Budget.QuestionRounds = 3 }},
		{"negative tokens", func(s *State) { s.Current.Budget.InputTokens = -1 }},
		{"used without plan", func(s *State) { s.Current.Used = []bool{true} }},
		{"proposal outside AWAITING_PLAN", func(s *State) { s.Current.ProposedPlan = planned.Current.ProposedPlan }},
		{"updated before created", func(s *State) { s.UpdatedAt = t0.Add(-time.Hour) }},
		{"turn time missing", func(s *State) { s.Current.StartedAt = time.Time{} }},
		{"zero seen id", func(s *State) { s.Seen = append(s.Seen, values.Ref{}) }},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			s := th.State()
			s.Version = 3
			cur := *s.Current
			s.Current = &cur
			b.mutate(&s)
			if _, err := Restore(s); !errors.Is(err, ErrInvalidThread) {
				t.Fatalf("Restore error = %v, want ErrInvalidThread", err)
			}
		})
	}
}

func TestTurnViewIsACopy(t *testing.T) {
	th := reach(t, StatusAwaitingPlan)
	turn, _ := th.Turn()
	p := turn.ProposedPlan()
	p[0] = values.PlanStep{}
	again, _ := th.Turn()
	if again.ProposedPlan()[0].Tool() == "" {
		t.Fatal("Turn view shares the plan with the aggregate")
	}
}
