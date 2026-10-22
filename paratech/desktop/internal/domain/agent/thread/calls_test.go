package thread

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

func callID(t *testing.T, id, tool, args string) values.Call {
	t.Helper()
	c, err := values.ParseCall(id, tool, args)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ok(id string) values.ToolResult { return values.ToolResult{CallID: id, ResultJSON: `{"ok":true}`} }

// stepWith opens a step, ends it with the given call IDs and records them.
func stepWith(t *testing.T, th *Thread, ids ...string) {
	t.Helper()
	if _, err := th.BeginStep(at(3)); err != nil {
		t.Fatal(err)
	}
	if err := th.EndStep(len(ids), 10, at(3)); err != nil {
		t.Fatal(err)
	}
	raw := make([]values.RawCall, len(ids))
	for i, id := range ids {
		raw[i] = values.RawCall{CallID: id, Tool: "list_subjects", ArgumentsJSON: `{}`}
	}
	if err := th.Expect(raw, at(3)); err != nil {
		t.Fatal(err)
	}
}

func TestExpect(t *testing.T) {
	bad := [][]values.RawCall{
		nil,
		{{CallID: "c1"}, {CallID: "c1"}},
		{{CallID: "c 1"}},
	}
	for i, calls := range bad {
		th := newThread(t)
		if err := th.Expect(calls, at(2)); !errors.Is(err, ErrInvalidThread) {
			t.Errorf("bad calls %d = %v", i, err)
		}
	}
	th := newThread(t)
	if err := th.Expect([]values.RawCall{{CallID: "c1", Tool: "show_plan"}, {CallID: "c2", Tool: "no_such"}}, at(2)); err != nil {
		t.Fatal(err)
	}
	turn, _ := th.Turn()
	if open := turn.OpenCalls(); open[0].Tool != values.ToolShowPlan || open[1].Tool != "" || open[0].PlanStep != -1 {
		t.Fatalf("open calls = %+v", open)
	}
	if err := th.Expect([]values.RawCall{{CallID: "c3"}}, at(2)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second set while the first waits = %v", err)
	}
}

func TestStartRequestScope(t *testing.T) {
	s1 := ref(t, values.KindSubject, "s1")
	scope, err := values.NewScope(s1, values.ScreenToday, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	none, err := New("th1", NoSubject, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := none.StartRequest(scope, at(1)); !errors.Is(err, ErrInvalidThread) {
		t.Fatalf("subject scope in the no-subject thread = %v", err)
	}
	key, _ := SubjectKey(s1)
	th, err := New("th2", key, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.StartRequest(scope, at(1)); err != nil {
		t.Fatal(err)
	}
	st := th.State()
	st.Version = 1
	back, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if turn, _ := back.Turn(); turn.Scope() != scope {
		t.Fatalf("restored scope = %+v", turn.Scope())
	}
	st.Current.Scope = values.Scope{}
	if _, err := Restore(st); !errors.Is(err, ErrInvalidThread) {
		t.Fatalf("restore with a scope of another thread = %v", err)
	}
}

func takeAll(t *testing.T, th *Thread) []values.ToolResult {
	t.Helper()
	res, done := th.TakeResults(at(4))
	if !done {
		t.Fatal("results are not complete")
	}
	return res
}

// approvedFolderPlan approves [create_folder A, place_upload into the created folder, create_folder B].
func approvedFolderPlan(t *testing.T) *Thread {
	t.Helper()
	th := newThread(t)
	steps := planSteps(t, `[{"tool":"create_folder","args":{"subject":"s1","name":"Папка А"}},`+
		`{"tool":"place_upload","args":{"upload":"u1","subject":"s1"}},`+
		`{"tool":"create_folder","args":{"subject":"s1","name":"Папка Б"}}]`)
	if err := th.ProposePlan(steps, context(t), at(2)); err != nil {
		t.Fatal(err)
	}
	if err := th.ApprovePlan(at(2)); err != nil {
		t.Fatal(err)
	}
	return th
}

// createFolder runs a create_folder call of the plan and answers it with the created folder.
func createFolder(t *testing.T, th *Thread, id, name, folder string) {
	t.Helper()
	stepWith(t, th, id)
	adm, err := th.Admit(callID(t, id, "create_folder", `{"subject":"s1","name":"`+name+`"}`), Facts{Context: context(t)}, at(3))
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	if adm.PlanStep < 0 {
		t.Fatal("create folder ran outside the plan")
	}
	if err := th.Answer(ok(id), ref(t, values.KindFolder, folder), at(3)); err != nil {
		t.Fatal(err)
	}
	takeAll(t, th)
}

func placeInto(t *testing.T, th *Thread, id, folder string) error {
	t.Helper()
	stepWith(t, th, id)
	_, err := th.Admit(callID(t, id, "place_upload", `{"upload":"u1","subject":"s1","folder":"`+folder+`"}`),
		Facts{Context: context(t)}, at(3))
	th.Abandon(values.RefuseNotDone, at(3))
	takeAll(t, th)
	return err
}

// Decision (a): a deferred plan argument binds only to an ID created in this request by an earlier approved step.
func TestDeferredIDBindsToCreatedID(t *testing.T) {
	t.Run("a known folder that was not created is refused", func(t *testing.T) {
		th := approvedFolderPlan(t)
		if err := th.Observe(ref(t, values.KindFolder, "old")); err != nil {
			t.Fatal(err)
		}
		createFolder(t, th, "c1", "Папка А", "fa")
		if err := placeInto(t, th, "c2", "old"); !errors.Is(err, ErrNotInPlan) {
			t.Fatalf("place into a folder not created by the plan = %v, want ErrNotInPlan", err)
		}
	})
	t.Run("the folder of a later create step is refused", func(t *testing.T) {
		th := approvedFolderPlan(t)
		createFolder(t, th, "c1", "Папка Б", "fb")
		if err := placeInto(t, th, "c2", "fb"); !errors.Is(err, ErrNotInPlan) {
			t.Fatalf("place into the folder of step 3 = %v, want ErrNotInPlan", err)
		}
	})
	t.Run("the folder created by the earlier step is bound", func(t *testing.T) {
		th := approvedFolderPlan(t)
		createFolder(t, th, "c1", "Папка А", "fa")
		if err := placeInto(t, th, "c2", "fa"); err != nil {
			t.Fatalf("place into the created folder = %v", err)
		}
		turn, _ := th.Turn()
		if got := turn.Created(); len(got) != 1 || got[0].Step != 0 || got[0].Ref.ID() != "fa" {
			t.Fatalf("created = %+v", got)
		}
		if !th.Seen(ref(t, values.KindFolder, "fa")) {
			t.Fatal("created folder not known to provenance")
		}
	})
	t.Run("a new plan forgets the created ids", func(t *testing.T) {
		th := approvedFolderPlan(t)
		createFolder(t, th, "c1", "Папка А", "fa")
		steps := planSteps(t, `[{"tool":"make_summary","args":{"recording":"r1"}}]`)
		if err := th.ProposePlan(steps, context(t), at(5)); err != nil {
			t.Fatal(err)
		}
		if err := th.ApprovePlan(at(5)); err != nil {
			t.Fatal(err)
		}
		if turn, _ := th.Turn(); len(turn.Created()) != 0 {
			t.Fatal("created ids kept across plans")
		}
	})
}

func TestAnswerRules(t *testing.T) {
	th := newThread(t)
	stepWith(t, th, "c1", "c2")
	if _, err := th.Admit(callID(t, "c1", "list_subjects", `{}`), Facts{}, at(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := th.BeginStep(at(3)); !errors.Is(err, ErrResultsPending) {
		t.Fatalf("BeginStep with pending results = %v", err)
	}
	if err := th.Answer(ok("zz"), values.Ref{}, at(3)); !errors.Is(err, ErrUnknownCall) {
		t.Fatalf("answer of an unknown call = %v", err)
	}
	if err := th.Answer(values.ToolResult{CallID: "c1", ResultJSON: "{"}, values.Ref{}, at(3)); !errors.Is(err, values.ErrInvalidResult) {
		t.Fatalf("answer with broken JSON = %v", err)
	}
	if err := th.Answer(ok("c1"), ref(t, values.KindSubject, "s9"), at(3)); !errors.Is(err, ErrInvalidThread) {
		t.Fatalf("a read call cannot create an id = %v", err)
	}
	if err := th.Answer(ok("c1"), values.Ref{}, at(3)); err != nil {
		t.Fatal(err)
	}
	if err := th.Answer(ok("c1"), values.Ref{}, at(3)); !errors.Is(err, ErrUnknownCall) {
		t.Fatalf("second answer = %v", err)
	}
	if _, done := th.TakeResults(at(3)); done {
		t.Fatal("results taken while c2 waits")
	}
	th.Abandon(values.RefuseNotDone, at(3))
	res := takeAll(t, th)
	if len(res) != 2 || res[0].CallID != "c1" || res[1].CallID != "c2" || !res[1].IsError {
		t.Fatalf("results = %+v", res)
	}
	if _, err := th.BeginStep(at(4)); err != nil {
		t.Fatalf("BeginStep after the results = %v", err)
	}
}

func TestStartTurnNeedsTakenResults(t *testing.T) {
	th := newThread(t)
	stepWith(t, th, "c1")
	if err := th.ProposePlan(planSteps(t, `[{"tool":"make_summary","args":{"recording":"r1"}}]`), context(t), at(3)); err != nil {
		t.Fatal(err)
	}
	if err := th.StartTurn(at(4)); !errors.Is(err, ErrResultsPending) {
		t.Fatalf("StartTurn with an open show_plan = %v", err)
	}
	if status(t, th) != StatusAwaitingPlan {
		t.Fatal("refused StartTurn changed the request")
	}
	th.Abandon(values.RefuseNotDone, at(4))
	takeAll(t, th)
	if err := th.StartTurn(at(5)); err != nil {
		t.Fatal(err)
	}
}

// Decision (b): paid reads do not count as the paid job without a plan; a paid write does.
func TestPaidReadsAreNotPaidJobs(t *testing.T) {
	th := newThread(t)
	f := Facts{Context: context(t), CostMicroUSD: 10_000}
	for i, c := range []values.Call{
		callID(t, "c1", "answer_question", `{"question":"Что такое энтропия"}`),
		callID(t, "c2", "inspect_upload", `{"upload":"u1"}`),
		callID(t, "c3", "answer_question", `{"question":"Что такое работа"}`),
	} {
		adm, err := th.Admit(c, f, at(3))
		if err != nil || !adm.Paid {
			t.Fatalf("paid read %d = %+v, %v", i, adm, err)
		}
	}
	if turn, _ := th.Turn(); turn.Budget().PaidUnplanned != 0 || turn.Budget().Writes != 0 {
		t.Fatalf("budget after paid reads = %+v", turn.Budget())
	}
	if _, err := th.Admit(callID(t, "c4", "make_summary", `{"recording":"r1"}`), f, at(3)); err != nil {
		t.Fatalf("cheap paid write = %v", err)
	}
	if turn, _ := th.Turn(); turn.Budget().PaidUnplanned != 1 {
		t.Fatalf("paid write not counted: %+v", turn.Budget())
	}
}

// Decision (d): «Продолжить» repeats the key; «Повторить» after a permanent failure gets a new deterministic key.
func TestRetryKeys(t *testing.T) {
	th := newThread(t)
	key, err := th.BeginStep(at(2))
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Stop(values.StopOffline, at(3)); err != nil {
		t.Fatal(err)
	}
	if err := th.Continue(at(3)); err != nil {
		t.Fatal(err)
	}
	if got, _ := th.StepKey(); got != key {
		t.Fatalf("key after Continue = %q, want %q", got, key)
	}

	for n := 1; n <= MaxRetries; n++ {
		if err := th.Stop(values.StopFailed, at(4)); err != nil {
			t.Fatal(err)
		}
		if err := th.Continue(at(4)); !errors.Is(err, ErrRetryRequired) {
			t.Fatalf("Continue after a failure = %v", err)
		}
		if err := th.Retry(at(4)); err != nil {
			t.Fatalf("retry %d: %v", n, err)
		}
		want := key + ":r" + string(rune('0'+n))
		if got, _ := th.StepKey(); got != want {
			t.Fatalf("key after retry %d = %q, want %q", n, got, want)
		}
	}
	if err := th.Stop(values.StopFailed, at(5)); err != nil {
		t.Fatal(err)
	}
	if err := th.Retry(at(5)); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("retry over the limit = %v", err)
	}
	if turn, _ := th.Turn(); turn.StopReason() != values.StopBudget {
		t.Fatalf("stop reason = %s, want budget", turn.StopReason())
	}

	// A new step starts without the retry suffix.
	th2 := newThread(t)
	if _, err := th2.BeginStep(at(2)); err != nil {
		t.Fatal(err)
	}
	if err := th2.Stop(values.StopFailed, at(2)); err != nil {
		t.Fatal(err)
	}
	if err := th2.Retry(at(2)); err != nil {
		t.Fatal(err)
	}
	if err := th2.EndStep(1, 1, at(2)); err != nil {
		t.Fatal(err)
	}
	if next, _ := th2.BeginStep(at(3)); next != "ag:th1:1:2" {
		t.Fatalf("next key = %q", next)
	}
	if err := th2.Retry(at(3)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("retry of a running step = %v", err)
	}
}

func TestRestartAfterExpiredTurn(t *testing.T) {
	th := newThread(t)
	if _, err := th.BeginStep(at(2)); err != nil {
		t.Fatal(err)
	}
	if err := th.Stop(values.StopExpired, at(3)); err != nil {
		t.Fatal(err)
	}
	if err := th.Continue(at(3)); !errors.Is(err, ErrRestartRequired) {
		t.Fatalf("Continue after expiry = %v", err)
	}
	if turn, _ := th.Turn(); !turn.CanRestartSilently() {
		t.Fatal("a fresh expired request cannot restart silently")
	}
	if err := th.Restart(at(4)); err != nil {
		t.Fatal(err)
	}
	turn, _ := th.Turn()
	if turn.Number() != 2 || turn.Status() != StatusPlanning || th.HistoryFrom() != 2 {
		t.Fatalf("restarted = %d %s from %d", turn.Number(), turn.Status(), th.HistoryFrom())
	}
	if err := th.Restart(at(5)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("restart of a running request = %v", err)
	}

	// A request that already wrote is not restarted without the student.
	busy := newThread(t)
	if _, err := busy.Admit(call(t, "create_subject", `{"name":"Физика"}`), Facts{}, at(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := busy.BeginStep(at(2)); err != nil {
		t.Fatal(err)
	}
	if err := busy.Stop(values.StopExpired, at(3)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := busy.Turn(); turn.CanRestartSilently() {
		t.Fatal("a request with a write restarts silently")
	}
}

func TestRefusalOf(t *testing.T) {
	tests := []struct {
		err  error
		want values.Refusal
	}{
		{ErrPlanRequired, values.RefusePlanRequired},
		{ErrNotInPlan, values.RefuseNotInPlan},
		{ErrTooManyRounds, values.RefuseTooManyRounds},
		{ErrInvalidPlan, values.RefuseInvalidPlan},
		{ErrBudgetExceeded, values.RefuseBudget},
		{values.ErrUnknownTool, values.RefuseUnknownTool},
		{values.ErrInvalidArguments, values.RefuseInvalidArgs},
	}
	for _, tt := range tests {
		if got, ok := RefusalOf(tt.err); !ok || got != tt.want {
			t.Errorf("RefusalOf(%v) = %q, %v", tt.err, got, ok)
		}
	}
	if _, ok := RefusalOf(ErrInvalidTransition); ok {
		t.Error("a transition error is a refusal")
	}
}

func TestRestoreKeepsCallsAndRetry(t *testing.T) {
	th := approvedFolderPlan(t)
	createFolder(t, th, "c1", "Папка А", "fa")
	stepWith(t, th, "c2", "c3")
	if err := th.Answer(ok("c3"), values.Ref{}, at(3)); err != nil {
		t.Fatal(err)
	}
	st := th.State()
	st.Version = 4
	got, err := Restore(st)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	turn, _ := got.Turn()
	if len(turn.OpenCalls()) != 2 || !turn.OpenCalls()[1].Answered || len(turn.Created()) != 1 {
		t.Fatalf("restored calls = %+v, created %+v", turn.OpenCalls(), turn.Created())
	}

	bad := []struct {
		name string
		mut  func(s *State)
	}{
		{"duplicate open call", func(s *State) { s.Current.Open = append(s.Current.Open, s.Current.Open[0]) }},
		{"answered without result", func(s *State) { s.Current.Open[0].Answered = true }},
		{"created outside the plan", func(s *State) { s.Current.Created[0].Step = 9 }},
		{"created of a wrong kind", func(s *State) { s.Current.Created[0].Ref = ref(t, values.KindJob, "j") }},
		{"retry out of range", func(s *State) { s.Current.Retry = MaxRetries + 1 }},
		{"history start out of range", func(s *State) { s.HistoryFrom = 5 }},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			s := th.State()
			s.Version = 4
			s.Current.Open = th.State().Current.Open
			tt.mut(&s)
			if _, err := Restore(s); !errors.Is(err, ErrInvalidThread) {
				t.Fatalf("Restore = %v, want ErrInvalidThread", err)
			}
		})
	}
}

func entry(turn int, text string, calls *values.SealedCalls, results ...values.ToolResult) values.HistoryEntry {
	return values.HistoryEntry{Turn: turn, Entry: values.HistoryTurn{StudentText: text, Calls: calls, Results: results}}
}

func TestWindow(t *testing.T) {
	sealed := &values.SealedCalls{Calls: []values.RawCall{{CallID: "c", Tool: "reply", ArgumentsJSON: `{}`}}, Seal: []byte{1}}
	var entries []values.HistoryEntry
	for turn := 1; turn <= 8; turn++ {
		entries = append(entries, entry(turn, "просьба", nil), entry(turn, "", sealed), entry(turn, "", nil, ok("c")))
	}
	got := Window(entries, 8)
	// 12 earlier entries at most: four whole requests before the current one.
	if len(got) != 15 || got[0].StudentText == "" {
		t.Fatalf("window has %d entries, first %+v", len(got), got[0])
	}

	// The current request is always whole, even when it alone is above the limits.
	var long []values.HistoryEntry
	long = append(long, entry(1, "просьба", nil))
	for range 10 {
		long = append(long, entry(1, "", sealed), entry(1, "", nil, ok("c")))
	}
	if got := Window(long, 1); len(got) != len(long) {
		t.Fatalf("current request cut to %d entries", len(got))
	}
}
