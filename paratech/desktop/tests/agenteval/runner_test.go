package agenteval

// The runner of the live check. It is untagged so CI compiles it and tests it on the fake Hub; only TestAgentLive
// (live_test.go) talks to a real Hub.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// maxLiveSteps bounds the planner steps of one request; the run stops at the first card or action.
const maxLiveSteps = 4

// liveToday is the date the canned results are made for.
var liveToday = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

// cannedResults are what read tools return in the run (fake data, the shapes the Driver will send).
var cannedResults = map[string]string{
	// The names are those of the case file: the subject question of catalog v8 offers them.
	"list_subjects": `{"subjects":[{"id":"s-math","name":"Математический анализ"},{"id":"s-phys","name":"Физика"},` +
		`{"id":"s-prog","name":"Программирование"},{"id":"s-hist","name":"История"}]}`,
	"find_lessons":  `{"lessons":[{"id":"l-7","date":"2026-10-21","kind":"lecture","recording":"rec-7","has_summary":false,"has_text":false}]}`,
	"list_assignments": `{"assignments":[{"id":"a-12","due":"2026-10-24","status":"upcoming","subject":"s-math"},` +
		`{"id":"a-20","due":"2026-10-27","status":"upcoming","subject":"s-phys"}]}`,
	"job_status":      `{"jobs":[{"id":"j-1","status":"running","done":3,"total":8}]}`,
	"delivery_status": `{"status":"not_sent"}`,
	"search":          `{"lectures":2,"materials":1}`,
	"answer_question": `{"answer_id":"an-1","claims":2,"not_in_materials":false}`,
	"inspect_upload":  `{"kind":"assignment","subject":"s-prog","has_homework":true,"mentions_email":false}`,
}

// liveRun sends requests of the case set to a Hub through the real Hub client.
type liveRun struct {
	client  *hubclient.Client
	classes map[string]string
	base    values.Base
	id      string // makes the keys of this run unique
}

func liveBase(subjects []Subject) (values.Base, error) {
	var out []values.BaseSubject
	for _, s := range subjects {
		ref, err := values.NewRef(values.KindSubject, s.ID)
		if err != nil {
			return values.Base{}, err
		}
		out = append(out, values.BaseSubject{ID: ref, Name: values.Name(s.Name)})
	}
	today, err := values.NewDate(liveToday.Year(), liveToday.Month(), liveToday.Day())
	if err != nil {
		return values.Base{}, err
	}
	return values.NewBase(out, today, "Europe/Moscow")
}

// request runs one request until the planner shows a card or proposes an action. Nothing runs: read tools get canned
// results, and the first action or card ends the request; only the first ask_questions of a case with an Answer is
// answered (catalog v8) and the run goes on.
func (r *liveRun) request(req Request) Outcome {
	o := Outcome{Case: req}
	scope, text, err := liveInput(req)
	if err != nil {
		o.Err = err.Error()
		return o
	}
	thread := shared.ThreadID("live-" + r.id + "-" + req.ID)
	history := []values.HistoryTurn{{StudentText: text}}
	answered := false
	for step := 1; step <= maxLiveSteps; step++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		reply, err := r.client.Step(ctx, values.StepRequest{
			Key: fmt.Sprintf("ag:%s:1:%d", thread, step), Thread: thread, Base: r.base, Scope: scope, History: history,
			MaxOutputTokens: agentturn.MaxOutputTokens,
		})
		cancel()
		if err != nil {
			o.Err = err.Error()
			return o
		}
		stop := false
		results := make([]values.ToolResult, 0, len(reply.Calls.Calls))
		for _, c := range reply.Calls.Calls {
			o.Tools = append(o.Tools, c.Tool)
			o.Texts = append(o.Texts, CardTexts(c.Tool, c.ArgumentsJSON)...)
			o.Listed = append(o.Listed, ReplyAssignments(c.Tool, c.ArgumentsJSON)...)
			if s := TriageSource(c.Tool, c.ArgumentsJSON); s != "" {
				o.Sources = append(o.Sources, s)
			}
			if s := AssignedSubject(c.Tool, c.ArgumentsJSON); s != "" {
				o.Assigned = append(o.Assigned, s)
			}
			result, ok := cannedResults[c.Tool]
			switch {
			case c.Tool == string(values.ToolAskQuestions) && req.Answer != "" && !answered:
				if result, err = liveAnswer(c.CallID, c.ArgumentsJSON, req.Answer); err != nil {
					o.Err = err.Error()
					return o
				}
				answered = true
			case r.classes[c.Tool] != "R":
				stop, result = true, `{"ok":true}`
			case !ok:
				result = `{"items":[]}`
			}
			results = append(results, values.ToolResult{CallID: c.CallID, ResultJSON: result})
		}
		if stop {
			return o
		}
		calls := reply.Calls
		history = append(history, values.HistoryTurn{Calls: &calls}, values.HistoryTurn{Results: results})
	}
	return o
}

// liveAnswer is the result of an ask_questions call when the student picks option on its first question, encoded by
// the core as the Driver sends it.
func liveAnswer(callID, argumentsJSON, option string) (string, error) {
	i := AnswerOption(argumentsJSON, option)
	if i < 0 {
		return "", fmt.Errorf("ask_questions has no option %q", option)
	}
	res, err := values.AnswersResult(callID, []values.Answer{{Question: 0, Option: i}})
	if err != nil {
		return "", err
	}
	return res.ResultJSON, nil
}

func liveInput(req Request) (values.Scope, string, error) {
	var subject, assignment values.Ref
	var err error
	if req.Subject != "" {
		if subject, err = values.NewRef(values.KindSubject, req.Subject); err != nil {
			return values.Scope{}, "", err
		}
	}
	if req.Assignment != "" {
		if assignment, err = values.NewRef(values.KindAssignment, req.Assignment); err != nil {
			return values.Scope{}, "", err
		}
	}
	scope, err := values.NewScope(subject, values.ScreenToday, values.Ref{}, assignment)
	if err != nil {
		return values.Scope{}, "", err
	}
	var uploads []values.Ref
	for _, u := range req.Uploads {
		ref, err := values.NewRef(values.KindUpload, u)
		if err != nil {
			return values.Scope{}, "", err
		}
		uploads = append(uploads, ref)
	}
	text, err := values.NewStudentText(req.Text, uploads)
	if err != nil {
		return values.Scope{}, "", err
	}
	return scope, text, nil
}

// TestRunnerOnTheFakeHub: the runner sends every case of the file in a shape the Hub client accepts, feeds canned
// results to read tools, stops at the first action or card and reports an unreachable Hub as a run error.
func TestRunnerOnTheFakeHub(t *testing.T) {
	cs := loadCases(t)
	hub := testenv.NewFakeAgentHub()
	url, httpClient := testenv.StartFakeAgentHub(t, hub)
	creds := testenv.NewMemCredentials()
	if err := creds.Set(t.Context(), hubclient.DefaultCredentialName, "la_test_0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	client, err := hubclient.New(hubclient.Config{BaseURL: url, HTTPClient: httpClient, RequestTimeout: 5 * time.Second}, creds)
	if err != nil {
		t.Fatal(err)
	}
	r := &liveRun{client: client, classes: catalogClasses(t), id: "t1"}
	if r.base, err = liveBase(cs.Subjects); err != nil {
		t.Fatal(err)
	}

	hub.Then(testenv.AgentCall{ID: "toolu_1", Tool: "find_lessons", Args: `{"subject":"s-hist"}`}).
		Then(testenv.AgentCall{ID: "toolu_2", Tool: "make_summary", Args: `{"recording":"rec-7"}`}, testenv.AgentCall{ID: "toolu_3", Tool: "reply", Args: `{"text":"Я запустил конспект."}`})
	o := r.request(cs.Requests[slicesIndex(cs.Requests, "w04")])
	if o.Err != "" || fmt.Sprint(o.Tools) != "[find_lessons make_summary reply]" || len(o.Texts) != 1 {
		t.Fatalf("outcome = %+v", o)
	}
	reqs := hub.Requests()
	if len(reqs) != 2 || reqs[0].GetScope().GetSubjectId() != "s-hist" || len(reqs[0].GetBase().GetSubjects()) != len(cs.Subjects) ||
		reqs[1].GetTurns()[2].GetResults().GetResults()[0].GetResultJson() != cannedResults["find_lessons"] {
		t.Fatalf("requests = %v", reqs)
	}
	if ClassOf(o.Tools, r.classes) != ClassWrite || len(FirstPersonWords(o.Texts[0])) == 0 {
		t.Fatal("the outcome does not score as a write with a first-person text")
	}

	// Since catalog v3: «что горит на неделе» lists the assignments of the result in its reply; the reply text names none.
	hub.Then(testenv.AgentCall{ID: "toolu_4", Tool: "list_assignments", Args: `{"due_before":"2026-10-28"}`}).
		Then(testenv.AgentCall{ID: "toolu_5", Tool: "reply", Args: `{"text":"Два задания, ближайший срок — 2026-10-24.","assignments":["a-12","a-20"]}`})
	b03 := cs.Requests[slicesIndex(cs.Requests, "b03")]
	o = r.request(b03)
	if o.Err != "" || fmt.Sprint(o.Tools) != "[list_assignments reply]" || fmt.Sprint(o.Listed) != "[a-12 a-20]" {
		t.Fatalf("outcome = %+v", o)
	}
	if m := (Report{Classes: r.classes, Requests: []Outcome{o}}).Metrics(); m.ClassOK != 1 || m.ToolsOK != 1 || m.ListsOK != 1 || m.Address != 0 {
		t.Fatalf("metrics = %+v", m)
	}
	if reqs := hub.Requests(); reqs[len(reqs)-1].GetCatalogVersion() != values.CatalogVersion {
		t.Fatalf("catalog version = %d", reqs[len(reqs)-1].GetCatalogVersion())
	}

	// Catalog v4: a draft of an assignment goes to triage_assignment (generate_draft is gone), and a work on a topic
	// without an assignment to triage_assignment{topic}; the action ends the run.
	hub.Then(testenv.AgentCall{ID: "toolu_6", Tool: "triage_assignment", Args: `{"assignment":"a-31"}`}).
		Then(testenv.AgentCall{ID: "toolu_7", Tool: "triage_assignment", Args: `{"topic":"Реформы Петра I"}`})
	var v4 []Outcome
	for _, id := range []string{"w08", "b05"} {
		o := r.request(cs.Requests[slicesIndex(cs.Requests, id)])
		if o.Err != "" || fmt.Sprint(o.Tools) != "[triage_assignment]" {
			t.Fatalf("%s: outcome = %+v", id, o)
		}
		v4 = append(v4, o)
	}
	if m := (Report{Classes: r.classes, Requests: v4}).Metrics(); m.ClassOK != 2 || m.ToolsOK != 2 || m.Sources != 2 || m.SourcesOK != 2 {
		t.Fatalf("v4 metrics = %+v", m)
	}

	// Catalog v6: a request of the owner adds an assignment (the subject named, from the base); the action ends the
	// run, no send is proposed. Its other request (w13) names no subject, so since v8 it is asked first (below).
	hub.Then(testenv.AgentCall{ID: "toolu_8", Tool: "create_assignment", Args: `{"title":"Решить номер 5","subject":"s-math","due":"2026-10-30"}`})
	o = r.request(cs.Requests[slicesIndex(cs.Requests, "w12")])
	if o.Err != "" || fmt.Sprint(o.Tools) != "[create_assignment]" || fmt.Sprint(o.Assigned) != "[s-math]" {
		t.Fatalf("w12: outcome = %+v", o)
	}
	if m := (Report{Classes: r.classes, Requests: []Outcome{o}}).Metrics(); m.ClassOK != 1 || m.ToolsOK != 1 || m.AssignedOK != 1 {
		t.Fatalf("v6 metrics = %+v", m)
	}

	// Catalog v8: no subject in the thread and none named, so the subject is asked; the run answers with the index of
	// the case's option, and create_assignment gets that subject or none after «Без предмета» (the last option).
	subjectQuestion := `{"questions":[{"kind":"choice","question":"` + values.SubjectQuestion + `",` +
		`"options":["Математический анализ","Физика","Программирование","История","` + values.NoSubjectOption + `"]}]}`
	type answered struct {
		option int    // the index the run sends
		create string // the arguments the planner then gives
	}
	v8 := map[string]answered{
		"s01": {0, `{"title":"Решить номер 5","subject":"s-math","due":"2026-10-23"}`},
		"s02": {4, `{"title":"Решить номер 5","due":"2026-10-23"}`},
		"w13": {4, `{"title":"Скинуть проверочную Иванову","due":"2026-10-23","time":"09:00"}`},
	}
	var asked []Outcome
	for _, id := range []string{"s01", "s02", "w13"} {
		hub.Then(testenv.AgentCall{ID: "toolu_10", Tool: "list_subjects", Args: `{}`}).
			Then(testenv.AgentCall{ID: "toolu_11", Tool: "ask_questions", Args: subjectQuestion}).
			Then(testenv.AgentCall{ID: "toolu_12", Tool: "create_assignment", Args: v8[id].create})
		o := r.request(cs.Requests[slicesIndex(cs.Requests, id)])
		if o.Err != "" || fmt.Sprint(o.Tools) != "[list_subjects ask_questions create_assignment]" {
			t.Fatalf("%s: outcome = %+v", id, o)
		}
		reqs := hub.Requests()
		results := reqs[len(reqs)-1].GetTurns()
		if got := results[len(results)-1].GetResults().GetResults()[0].GetResultJson(); got != fmt.Sprintf(`{"answers":[{"question":0,"option":%d}]}`, v8[id].option) {
			t.Fatalf("%s: answer = %s", id, got)
		}
		asked = append(asked, o)
	}
	if m := (Report{Classes: r.classes, Requests: asked}).Metrics(); m.ClassOK != 3 || m.ToolsOK != 3 || m.Assigned != 3 || m.AssignedOK != 3 {
		t.Fatalf("v8 metrics = %+v", m)
	}

	// An option the question does not offer ends the run with an error instead of a guessed answer.
	hub.Then(testenv.AgentCall{ID: "toolu_13", Tool: "ask_questions", Args: `{"questions":[{"kind":"choice","question":"Q","options":["a","b"]}]}`})
	if o := r.request(cs.Requests[slicesIndex(cs.Requests, "s04")]); o.Err == "" || fmt.Sprint(o.Tools) != "[ask_questions]" {
		t.Fatalf("s04 without its option: outcome = %+v", o)
	}

	// Every case of the file can be sent: the fake Hub has no script left and answers unavailable. A new run ID, so
	// w04 is not a replay of the first run.
	r.id = "t2"
	for _, req := range append(append([]Request(nil), cs.Requests...), cs.CannotDo...) {
		if _, _, err := liveInput(req); err != nil {
			t.Fatalf("%s: %v", req.ID, err)
		}
		if o := r.request(req); o.Err == "" || len(o.Tools) != 0 {
			t.Fatalf("%s: outcome %+v, want a run error from the Hub", req.ID, o)
		}
	}
}

func slicesIndex(reqs []Request, id string) int {
	for i, r := range reqs {
		if r.ID == id {
			return i
		}
	}
	return -1
}
