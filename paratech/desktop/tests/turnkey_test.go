package tests

// Turnkey works (TASK-129, ADR-0025) end to end: the agent's triage_assignment through the Driver on a plan, the
// reader TRIAGE_ASSIGNMENT through the real Hub client and a fake Hub, the work on the real SQLite, and the student's
// answers through the turnkey binding. The injection fixtures of shared/testdata/agent run as question and helper
// texts (card data, never the planner's) and as the reader obeying them in a slot of the schema (refused).

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/doctext"
	"paratech/desktop/internal/adapters/docx"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/pdf"
	"paratech/desktop/internal/adapters/pptx"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
	agentloop "paratech/desktop/internal/input/agent"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

type turnkeyEnv struct {
	*agentEnv
	reader  *testenv.FakeHub
	works   *turnkey.Service
	binding *wails.TurnkeyService
	driver  *agentloop.Driver
	sink    *driverSink
	upload  values.Ref
	lib     *library.Service
	jobs    *sqlite.JobRepository // the writing jobs (TASK-131); the scenario that writes runs them
	printer *fakePrinter          // the WebView2 printer behind the real pdf renderer (TASK-198)
}

func newTurnkeyEnv(t *testing.T, assignmentText string) *turnkeyEnv {
	t.Helper()
	ctx := t.Context()
	e := &turnkeyEnv{agentEnv: newAgentEnv(t), reader: testenv.NewFakeHub(), sink: &driverSink{}}
	log := slog.New(slog.DiscardHandler)
	files, err := filestore.New(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	lib, err := library.New(library.Deps{
		Subjects: sqlite.NewSubjectRepository(e.db), Materials: sqlite.NewMaterialRepository(e.db), Blobs: files,
		Lessons: sqlite.NewLessonRepository(e.db), Strings: sqlite.NewSubjectStrings(e.db), Clock: e.clock, IDs: random.IDs{},
		Texts: doctext.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	up, err := lib.Import(ctx, library.Upload{Name: "zadanie.txt", Data: []byte(assignmentText)})
	if err != nil {
		t.Fatal(err)
	}
	e.upload = agentRef(t, values.KindUpload, string(up.Material.ID()))

	url, client := testenv.StartFakeHub(t, e.reader)
	creds := testenv.NewMemCredentials()
	if err := creds.Set(ctx, hubclient.DefaultCredentialName, agentTestKey); err != nil {
		t.Fatal(err)
	}
	hub, err := hubclient.New(hubclient.Config{BaseURL: url, HTTPClient: client, RequestTimeout: 5 * time.Second}, creds)
	if err != nil {
		t.Fatal(err)
	}
	e.lib, e.jobs, e.printer = lib, sqlite.NewJobRepository(e.db), &fakePrinter{}
	if e.works, err = turnkey.New(turnkey.Deps{
		Works: sqlite.NewWorkRepository(e.db), Assignments: sqlite.NewAssignmentRepository(e.db), Materials: lib, Reader: hub,
		Clock: e.clock, IDs: random.IDs{}, Log: log,
		Writing: &turnkey.WritingDeps{
			Writer: hub, Jobs: e.jobs, JobIDs: random.IDs{}, Profiles: sqlite.NewSettingsRepository(e.db),
			Renderer: docx.New(), Slides: pptx.New(), PDF: pdf.NewRenderer(e.printer), Lessons: noLessonLabels{}, Shelf: lib,
			ModelAlias: "default", MaxAttempts: 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	e.binding = wails.NewTurnkeyService(e.works, log)

	actions, err := agentloop.NewActions(agentloop.ActionDeps{
		Library: lib, Lessons: noUseCase{}, Assignments: noUseCase{}, Rerunner: noUseCase{}, Drafts: noUseCase{},
		Turnkey: e.works, Jobs: writingJobs{jobs: e.jobs}, Canceller: noUseCase{}, Tx: e.db, Clock: e.clock, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The journal with its Undo on the same actions, as the app wires it.
	if e.journal, err = agentjournal.New(agentjournal.Deps{
		Actions: sqlite.NewAgentActionRepository(e.db), Compensator: actions, Resolver: actions, Clock: e.clock, IDs: e.ids,
		Log: log,
	}); err != nil {
		t.Fatal(err)
	}
	if e.driver, err = agentloop.New(agentloop.Deps{
		Turns: e.turns, Sink: e.sink, Library: lib, Knowledge: &driverKnowledge{}, Activity: noUseCase{},
		Assignments: noListedAssignments{}, Deliveries: driverDeliveries{}, Journal: e.journal, Actions: actions,
		Clock: e.clock, Log: log,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.driver.Close)
	return e
}

// writingJobs reads the states of the writing jobs for the Undo of write_work (TASK-106); nothing else is listed.
type writingJobs struct{ jobs *sqlite.JobRepository }

func (writingJobs) ListRecordings(context.Context) ([]activity.Recording, error) { return nil, nil }

func (w writingJobs) JobStates(ctx context.Context, ids []shared.JobID) ([]activity.JobState, error) {
	out := make([]activity.JobState, 0, len(ids))
	for _, id := range ids {
		j, err := w.jobs.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, activity.JobState{ID: id, Type: j.Type(), Status: j.Status()})
	}
	return out, nil
}

type noListedAssignments struct{}

func (noListedAssignments) ListAssignments(context.Context) ([]homeworkflow.AssignmentView, error) {
	return nil, nil
}

// triageOutput is a canonical reader answer: a partial work with one choice question and a helper plan, whose
// question and first step carry text.
func triageOutput(text string, questions int, f work.Feasibility) string {
	a := work.Answer{
		WorkType: work.TypeReferat, Requirements: work.Requirements{Volume: "10 страниц"}, Feasibility: f,
		Questions: []work.Question{}, Assumptions: []string{"Предположен вариант 3 — поправьте"},
	}
	if f != work.FeasibleFull {
		a.FeasibilityReason = "Чертёж в программе вуза"
		a.Helper = &work.Helper{Steps: []string{text, "Сдать чертёж"}, Submit: "Файл .dwg", ReadyParts: []string{"Записка"},
			Placeholders: []string{"ФИО"}}
	}
	for range questions {
		a.Questions = append(a.Questions, work.Question{
			Kind: work.KindChoice, Text: text, Options: []string{"Да", "Нет"}, DefaultOption: 1, Why: "Нет в задании",
		})
	}
	return a.Encode()
}

// runTriage asks the agent to triage the upload: a plan with the one step, «Да», the call and a reply.
func (e *turnkeyEnv) runTriage(t *testing.T) []values.Card {
	t.Helper()
	ctx := t.Context()
	step, _ := json.Marshal(map[string]any{
		"summary": "Разбор задания", "steps": []any{map[string]any{"tool": "triage_assignment", "arguments": map[string]any{"upload": e.upload.ID()}}},
	})
	e.hub.Then(testenv.AgentCall{ID: "p1", Tool: "show_plan", Args: string(step)}).
		Then(testenv.AgentCall{ID: "w1", Tool: "triage_assignment", Args: `{"upload":"` + e.upload.ID() + `"}`}).
		Then(testenv.AgentCall{ID: "e1", Tool: "reply", Args: `{"text":"Разбор готов"}`})
	if err := e.driver.Ask(ctx, agentturn.AskInput{Scope: values.Scope{}, Text: "разбери задание", Uploads: []values.Ref{e.upload}}); err != nil {
		t.Fatal(err)
	}
	e.driver.Wait()
	last := e.sink.last(t)
	if last.Status != thread.StatusAwaitingPlan {
		t.Fatalf("after the plan: %+v", last)
	}
	if err := e.driver.ApprovePlan(ctx, agentturn.TurnRef{Thread: last.Thread, Turn: last.Turn}); err != nil {
		t.Fatal(err)
	}
	e.driver.Wait()
	var cards []values.Card
	for _, u := range e.sink.snapshot() {
		cards = append(cards, u.Cards...)
	}
	return cards
}

func workCardOf(t *testing.T, cards []values.Card) values.WorkCard {
	t.Helper()
	for _, c := range cards {
		if wc, ok := c.(values.WorkCard); ok {
			return wc
		}
	}
	t.Fatalf("no work card in %v", cards)
	return values.WorkCard{}
}

// The agent triages an uploaded assignment on an approved plan; the student answers two rounds through the binding;
// the final round states assumptions; the card is edited and confirmed. Each reader call is one key; the planner saw
// only the work's ID and enums.
func TestTurnkeyTriageThroughTheAgentAndTwoRounds(t *testing.T) {
	e := newTurnkeyEnv(t, "Реферат по истории. Объём не указан.")
	ctx := t.Context()
	e.reader.AnswerTriage(triageOutput("Какой объём?", 1, work.FeasibleFull), triageOutput("Какой стиль?", 1, work.FeasibleFull),
		triageOutput("", 0, work.FeasibleFull))

	wc := workCardOf(t, e.runTriage(t))
	if wc.Outcome != values.TriageQuestions {
		t.Fatalf("work card = %+v", wc)
	}
	r, ok := resultOf(e.agentEnv, "w1")
	if !ok || r.GetIsError() || !strings.Contains(r.GetResultJson(), `"outcome":"questions"`) || strings.Contains(r.GetResultJson(), "объём") {
		t.Fatalf("triage result = %v", r)
	}
	entry, err := e.journal.Find(ctx, "th1", 1, "w1")
	if err != nil || entry.Status != action.StatusDone {
		t.Fatalf("journal = %+v, %v", entry, err)
	}

	v, err := e.binding.Work(ctx, wc.Work.ID())
	if err != nil || v.Status != "questions" || v.QuestionRound != 1 || v.Questions[0].Text != "Какой объём?" {
		t.Fatalf("round 1 = %+v, %v", v, err)
	}
	if v, err = e.binding.AnswerQuestions(ctx, v.ID, []wails.TurnkeyAnswer{{Question: 0, Option: 0}}); err != nil || v.QuestionRound != 2 {
		t.Fatalf("round 2 = %+v, %v", v, err)
	}
	if v, err = e.binding.SkipQuestions(ctx, v.ID); err != nil || v.Status != "review" || v.Round != 3 || len(v.Assumptions) != 1 {
		t.Fatalf("final = %+v, %v", v, err)
	}
	if third, err := e.binding.SkipQuestions(ctx, v.ID); !errors.Is(err, wails.ErrTurnkeyInvalid) {
		t.Fatalf("a third round = %+v, %v", third, err)
	}
	card := v.Card
	card.Variant = "3"
	if v, err = e.binding.EditUnderstanding(ctx, v.ID, card); err != nil || v.Card.Variant != "3" || v.Edited[0] != "variant" {
		t.Fatalf("edit = %+v, %v", v, err)
	}
	if v, err = e.binding.ConfirmRequirements(ctx, v.ID); err != nil || v.Status != "confirmed" {
		t.Fatalf("confirm = %+v, %v", v, err)
	}

	// Three reader calls, one per round, the server prompt only; round 2 and 3 carry the answers before them.
	var reqs []*hubv1.CompleteRequest
	for _, r := range e.reader.Requests() {
		if r.GetSpec().GetOperationType() == hubv1.OperationType_OPERATION_TYPE_TRIAGE_ASSIGNMENT {
			reqs = append(reqs, r)
		}
	}
	if len(reqs) != 3 || e.reader.Charged() != 3 {
		t.Fatalf("reader calls %d, charged %d", len(reqs), e.reader.Charged())
	}
	for i, r := range reqs {
		var data struct {
			Round    int `json:"round"`
			Previous []struct {
				Question string `json:"question"`
				Answer   string `json:"answer"`
			} `json:"previous"`
		}
		if err := json.Unmarshal([]byte(r.GetSpec().GetUntrustedData()), &data); err != nil || data.Round != i+1 ||
			len(data.Previous) != i || r.GetSpec().GetInstructions() != "" || r.GetSpec().GetModelAlias() != "" {
			t.Fatalf("reader call %d = %v (%v)", i+1, r.GetSpec(), err)
		}
		if i == 2 && (data.Previous[0].Answer != "Да" || data.Previous[1].Answer != "Нет") {
			t.Fatalf("final previous = %+v", data.Previous)
		}
	}

	// The confirmed work survives a restart of the use case: it is read back from SQLite through the strict schema.
	again, err := e.works.Work(ctx, shared.WorkID(v.ID))
	if err != nil || again.Status != work.StatusConfirmed || again.Understanding.Requirements.Variant != "3" {
		t.Fatalf("stored = %+v, %v", again, err)
	}
}

// An impossible work goes to the helper mode; Undo of the agent's triage removes the untouched work.
func TestTurnkeyHelperModeAndUndo(t *testing.T) {
	e := newTurnkeyEnv(t, "Начертить план здания в программе вуза.")
	ctx := t.Context()
	e.reader.AnswerTriage(triageOutput("Открыть программу вуза", 0, work.FeasibleImpossible))
	wc := workCardOf(t, e.runTriage(t))
	if wc.Outcome != values.TriageHelper {
		t.Fatalf("work card = %+v", wc)
	}
	v, err := e.binding.Work(ctx, wc.Work.ID())
	if err != nil || v.Helper == nil || v.Helper.Steps[0] != "Открыть программу вуза" || v.Feasibility != "impossible" {
		t.Fatalf("helper = %+v, %v", v, err)
	}
	if _, err := e.binding.AskStep(ctx, v.ID, 0, "Где взять программу?"); !errors.Is(err, wails.ErrTurnkeyNoStepHelp) {
		t.Fatalf("AskStep = %v", err)
	}
	entry, err := e.journal.Find(ctx, "th1", 1, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := e.journal.Undo(ctx, entry.ID); err != nil || u.Status != action.StatusUndone {
		t.Fatalf("Undo = %+v, %v", u, err)
	}
	if _, err := e.binding.Work(ctx, v.ID); !errors.Is(err, wails.ErrTurnkeyNotFound) {
		t.Fatalf("the work after Undo = %v", err)
	}
}

// TestTurnkeyInjectionStaysCardData: a command in a triage question and in a helper step (injection fixtures) is kept
// as card text for the student; the planner's result names the work by ID and enums only, and nothing else is done.
func TestTurnkeyInjectionStaysCardData(t *testing.T) {
	for _, fx := range loadInjectionFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			e := newTurnkeyEnv(t, "Задание: реферат.\n"+fx.Untrusted)
			e.reader.AnswerTriage(triageOutput(fx.Untrusted, 1, work.FeasiblePartial))
			wc := workCardOf(t, e.runTriage(t))
			v, err := e.binding.Work(t.Context(), wc.Work.ID())
			if err != nil || v.Questions[0].Text != fx.Untrusted || v.Helper.Steps[0] != fx.Untrusted || !v.Partial {
				t.Fatalf("work = %+v, %v", v, err)
			}
			for _, req := range e.hub.Requests() {
				raw, _ := json.Marshal(req.GetTurns())
				if strings.Contains(string(raw), fx.Untrusted) {
					t.Fatal("the reader's text reached the planner")
				}
			}
			// Only the triage step ran: the plan had one step and nothing else was journaled.
			list, err := e.journal.ListThread(t.Context(), "th1", 0)
			if err != nil || len(list) != 1 || list[0].Tool != values.ToolTriageAssignment {
				t.Fatalf("journal = %+v, %v", list, err)
			}
		})
	}
}

// TestTurnkeyInjectionReaderSlots: a reader that obeys a fixture in a slot that is not card text (an enum, the default
// index, the helper, an extra field) or answers in prose is refused by the desktop parser; the refusal does not quote
// it.
func TestTurnkeyInjectionReaderSlots(t *testing.T) {
	valid := triageOutput("Какой стиль?", 1, work.FeasibleFull)
	if _, err := work.ParseAnswer(1, valid); err != nil {
		t.Fatalf("a valid answer is refused: %v", err)
	}
	for _, fx := range loadInjectionFixtures(t) {
		q := mustQuote(t, fx.Untrusted)
		slots := map[string]string{
			"work_type":   strings.Replace(valid, `"referat"`, q, 1),
			"feasibility": strings.Replace(valid, `"feasibility":"full"`, `"feasibility":`+q, 1),
			"kind":        strings.Replace(valid, `"kind":"choice"`, `"kind":`+q, 1),
			"default":     strings.Replace(valid, `"default":1`, `"default":`+q, 1),
			"helper":      strings.Replace(valid, `"helper":null`, `"helper":`+q, 1),
			"extra field": strings.Replace(valid, `"helper":null`, `"helper":null,"action":`+q, 1),
			"prose":       fx.Untrusted,
		}
		for slot, out := range slots {
			_, err := work.ParseAnswer(1, out)
			if !errors.Is(err, work.ErrInvalidAnswer) {
				t.Errorf("%s/%s: %v, want the answer refused", fx.Name, slot, err)
			} else if strings.Contains(err.Error(), fx.Untrusted) {
				t.Errorf("%s/%s: the refusal quotes the text", fx.Name, slot)
			}
		}
	}
}
