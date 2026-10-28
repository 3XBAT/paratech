package tests

// Scenario tests of create_assignment (TASK-217, ADR-0029) end to end: the planner's call runs through the real step
// use case, the Driver, the journal and the homework use case on SQLite; only the Hub and the calendar are fakes (no
// job runner runs, so nothing leaves the machine). The two requests of the owner: «завтра в 9 скинуть проверочную
// Иванову» and «была пара по матанализу, нужно решить номер 5 к 30 октября».

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/doctext"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
	agentloop "paratech/desktop/internal/input/agent"
	"paratech/desktop/testenv"
)

// librarySubjectNames is the homeworkflow.SubjectNames the app wires: the current name of a library subject.
type librarySubjectNames struct{ lib *library.Service }

func (n librarySubjectNames) SubjectName(ctx context.Context, id shared.SubjectID) (string, error) {
	list, err := n.lib.Subjects(ctx)
	if err != nil {
		return "", err
	}
	for _, s := range list {
		if s.ID() == id {
			return s.Name().String(), nil
		}
	}
	return "", fmt.Errorf("subject %s: %w", id, shared.ErrNotFound)
}

type assignmentEnv struct {
	*agentEnv
	lib       *library.Service
	homework  *homeworkflow.Service
	proposals *sqlite.ProposalRepository
	dlv       *sqlite.DeliveryRepository
	calendar  *testenv.FakeCalendar
	driver    *agentloop.Driver
	sink      *driverSink
	math      *subject.Subject
}

func newAssignmentEnv(t *testing.T) *assignmentEnv {
	t.Helper()
	e := &assignmentEnv{agentEnv: newAgentEnv(t), sink: &driverSink{}, calendar: &testenv.FakeCalendar{}}
	log := slog.New(slog.DiscardHandler)
	files, err := filestore.New(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	if e.lib, err = library.New(library.Deps{
		Subjects: sqlite.NewSubjectRepository(e.db), Materials: sqlite.NewMaterialRepository(e.db), Blobs: files,
		Lessons: sqlite.NewLessonRepository(e.db), Strings: sqlite.NewSubjectStrings(e.db), Clock: e.clock, IDs: random.IDs{},
		Texts: doctext.New(), Reader: fixedReader(lectureAnswer), Inspections: sqlite.NewInspectionRepository(e.db),
	}); err != nil {
		t.Fatal(err)
	}
	if e.math, err = e.lib.CreateSubject(t.Context(), library.NewSubject{Name: "Математический анализ"}); err != nil {
		t.Fatal(err)
	}

	e.dlv = sqlite.NewDeliveryRepository(e.db)
	deliveries, err := senddelivery.New(senddelivery.Deps{
		Deliveries: e.dlv, History: e.dlv, Artifacts: files, Email: unreachableEmail{t}, Jobs: sqlite.NewJobRepository(e.db),
		Credentials: testenv.NewMemCredentials(), Clock: e.clock, IDs: &deliveryIDs{}, Tx: e.db,
	}, senddelivery.Config{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := proposal.NewRecipient(settingsRecipient)
	if err != nil {
		t.Fatal(err)
	}
	assignments := sqlite.NewAssignmentRepository(e.db)
	e.proposals = sqlite.NewProposalRepository(e.db)
	if e.homework, err = homeworkflow.New(homeworkflow.Deps{
		Proposals: e.proposals, Assignments: assignments, AssignmentList: assignments, Artifacts: files,
		Recipients: fixedRecipient{r}, Mailer: deliveries, Exporter: files, Calendar: e.calendar,
		Recordings: sqlite.NewRecordingRepository(e.db), Lessons: sqlite.NewLessonRepository(e.db), Clock: e.clock,
		IDs: &postIDs{}, Tx: e.db, Drafts: sqlite.NewDraftRepository(e.db), Waiting: e.proposals,
		Subjects: librarySubjectNames{e.lib},
	}); err != nil {
		t.Fatal(err)
	}

	subjects, err := agentloop.NewBaseSubjects(e.lib)
	if err != nil {
		t.Fatal(err)
	}
	if e.turns, err = agentturn.New(agentturn.Deps{
		Model: e.model, Threads: sqlite.NewAgentThreadRepository(e.db), History: sqlite.NewAgentHistory(e.db, e.clock),
		Subjects: subjects, Facts: zeroFacts{}, Tx: e.db, Clock: e.clock, IDs: e.ids, Log: log,
	}); err != nil {
		t.Fatal(err)
	}
	actions, err := agentloop.NewActions(agentloop.ActionDeps{
		Library: e.lib, Lessons: noUseCase{}, Assignments: e.homework, NewAssignments: e.homework, Rerunner: noUseCase{},
		Jobs: noUseCase{}, Canceller: noUseCase{}, Tx: e.db, Clock: e.clock, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.journal, err = agentjournal.New(agentjournal.Deps{
		Actions: sqlite.NewAgentActionRepository(e.db), Compensator: actions, Resolver: actions,
		Threads: sqlite.NewAgentThreadRepository(e.db), Processing: sqlite.NewJobRepository(e.db), Clock: e.clock,
		IDs: e.ids, Log: log,
	}); err != nil {
		t.Fatal(err)
	}
	if e.driver, err = agentloop.New(agentloop.Deps{
		Turns: e.turns, Sink: e.sink, Library: e.lib, Knowledge: &driverKnowledge{}, Activity: noUseCase{},
		Assignments: e.homework, Deliveries: deliveries, Journal: e.journal, Actions: actions,
		Proposals: e.homework, Deletions: noPreviews{}, Clock: e.clock, Log: log,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.driver.Close)
	return e
}

// addedAssignmentID is the ID the desktop derives for the assignment a create_assignment call adds: "ag-" and the
// first 16 bytes of sha256("ag:<thread>:<turn>:<call_id>") in hex (input/agent). Pinned here so a change is seen.
func addedAssignmentID(thread shared.ThreadID, turn int, callID string) shared.AssignmentID {
	sum := sha256.Sum256(fmt.Appendf(nil, "ag:%s:%d:%s", thread, turn, callID))
	return shared.AssignmentID("ag-" + hex.EncodeToString(sum[:16]))
}

// ask runs a request to its end and returns every card the panel got.
func (e *assignmentEnv) ask(t *testing.T, text string) []values.Card {
	t.Helper()
	e.sink.mu.Lock()
	before := len(e.sink.updates)
	e.sink.mu.Unlock()
	if err := e.driver.Ask(t.Context(), agentturn.AskInput{Scope: values.Scope{}, Text: text}); err != nil {
		t.Fatal(err)
	}
	e.driver.Wait()
	var cards []values.Card
	for _, u := range e.sink.snapshot()[before:] {
		cards = append(cards, u.Cards...)
	}
	return cards
}

// endCall is the reply that ends a request, with a call ID of its own.
func endCall(id string) testenv.AgentCall {
	return testenv.AgentCall{ID: id, Tool: "reply", Args: `{"text":"Готово."}`}
}

func actionLines(cards []values.Card) []values.ActionLine {
	var out []values.ActionLine
	for _, c := range cards {
		if l, ok := c.(values.ActionLine); ok {
			out = append(out, l)
		}
	}
	return out
}

// plannerResults are every result the planner got, by call ID.
func (e *assignmentEnv) plannerResults() map[string]string {
	out := map[string]string{}
	for _, req := range e.hub.Requests() {
		for _, turn := range req.GetTurns() {
			for _, r := range turn.GetResults().GetResults() {
				out[r.GetCallId()] = r.GetResultJson()
			}
		}
	}
	return out
}

// «завтра в 9 скинуть проверочную Иванову»: the assignment is added with the deadline tomorrow 09:00 and no subject,
// the planner gets its ID only, the panel line shows it from the local data, and «Отменить» removes it. Nothing is
// proposed: «Иванову» stays text, never an address.
func TestAgentAddsAReminderAndUndoesIt(t *testing.T) {
	e := newAssignmentEnv(t)
	ctx := t.Context()
	e.hub.Then(testenv.AgentCall{ID: "c1", Tool: "create_assignment",
		Args: `{"title":"Скинуть проверочную Иванову","due":"2026-10-23","time":"09:00"}`}).
		Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Задание добавлено, срок 2026-10-23."}`})
	cards := e.ask(t, "завтра в 9 скинуть проверочную Иванову")

	id := addedAssignmentID("th1", 1, "c1")
	v, err := e.homework.Assignment(ctx, id)
	if err != nil || v.Title != "Скинуть проверочную Иванову" || !v.Due.Equal(time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)) ||
		v.Subject != "" || v.Version != 1 {
		t.Fatalf("assignment = %+v, %v", v, err)
	}
	if got := e.plannerResults()["c1"]; got != `{"changed":true,"done":true,"id":"`+string(id)+`"}` {
		t.Fatalf("result = %s", got)
	}
	lines := actionLines(cards)
	if len(lines) != 1 || lines[0].Assignment.Title != v.Title || lines[0].Assignment.DueTime.String() != "09:00" ||
		lines[0].Assignment.Due.String() != "2026-10-23" || !lines[0].Assignment.Subject.IsZero() {
		t.Fatalf("action lines = %+v", lines)
	}
	if waiting, _ := e.proposals.ListByStatus(ctx, proposal.StatusValidated, 10); len(waiting) != 0 {
		t.Fatalf("proposals = %v", waiting)
	}

	entry, err := e.journal.Find(ctx, "th1", 1, "c1")
	if err != nil || entry.Status != action.StatusDone || !entry.CanUndo || lines[0].Action != entry.ID {
		t.Fatalf("entry = %+v, %v", entry, err)
	}
	if u, err := e.journal.Undo(ctx, entry.ID); err != nil || u.Status != action.StatusUndone {
		t.Fatalf("Undo = %+v, %v", u, err)
	}
	if list, err := e.homework.ListAssignments(ctx); err != nil || len(list) != 0 {
		t.Fatalf("assignments after Undo = %+v, %v", list, err)
	}
}

// «была пара по матанализу, нужно решить номер 5 к 30 октября»: the assignment is added in the library subject and
// listed with it; its new ID is accepted in propose_deadline of the same request (the calendar only on «Да»), and
// once that proposal waits the Undo of the creation is refused («Изменено после»).
func TestAgentAddsAnAssignmentInASubject(t *testing.T) {
	e := newAssignmentEnv(t)
	ctx := t.Context()
	id := addedAssignmentID("th1", 1, "c1")
	e.hub.Then(testenv.AgentCall{ID: "c1", Tool: "create_assignment",
		Args: `{"title":"Решить номер 5","subject":"` + string(e.math.ID()) + `","due":"2026-10-30"}`}).
		Then(testenv.AgentCall{ID: "d1", Tool: "propose_deadline", Args: `{"assignment":"` + string(id) + `"}`}).
		Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Задание добавлено, срок 2026-10-30."}`})
	cards := e.ask(t, "была пара по матанализу, нужно решить номер 5 к 30 октября")

	list, err := e.homework.ListAssignments(ctx)
	if err != nil || len(list) != 1 || list[0].ID != id || list[0].Subject != "Математический анализ" ||
		!list[0].Due.Equal(time.Date(2026, 10, 30, 23, 59, 0, 0, time.UTC)) {
		t.Fatalf("assignments = %+v, %v", list, err)
	}
	if lines := actionLines(cards); len(lines) != 1 || lines[0].Assignment.Subject.ID() != string(e.math.ID()) {
		t.Fatalf("action lines = %+v", lines)
	}
	targets := confirmTargets(cards)
	if len(targets) != 1 || targets[0].Kind() != values.KindProposal || len(e.calendar.Calls()) != 0 {
		t.Fatalf("confirm cards = %v, calendar %d", targets, len(e.calendar.Calls()))
	}

	entry, err := e.journal.Find(ctx, "th1", 1, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := e.journal.Undo(ctx, entry.ID); err != nil || u.Status != action.StatusUndoRefused || u.NoUndo != action.NoUndoChanged {
		t.Fatalf("Undo with a waiting calendar proposal = %+v, %v", u, err)
	}
	if _, err := e.homework.Assignment(ctx, id); err != nil {
		t.Fatalf("a refused Undo removed the assignment: %v", err)
	}
}

// Catalog 8 (TASK-233, TASK-234): a request that names no subject in a chat without one gets «К какому предмету
// относится задание?» with nothing preselected, so a fast click cannot file it under the first subject. An option
// index outside the question (-1 included) is refused and the card keeps waiting; the picked subject's ID, or none
// after «Без предмета», goes into create_assignment.
func TestAgentAsksTheSubjectOfAnAssignment(t *testing.T) {
	for _, tt := range []struct {
		name    string
		option  int
		subject string // the subject name the assignment gets, "" for none
	}{
		{"a subject", 0, "Математический анализ"},
		{"no subject", 1, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newAssignmentEnv(t)
			ctx := t.Context()
			question := `{"questions":[{"kind":"choice","question":"` + values.SubjectQuestion + `",` +
				`"options":["Математический анализ","` + values.NoSubjectOption + `"]}]}`
			e.hub.Then(testenv.AgentCall{ID: "l1", Tool: "list_subjects", Args: `{}`}).
				Then(testenv.AgentCall{ID: "q1", Tool: "ask_questions", Args: question})
			cards := e.ask(t, "запиши: решить номер 5 к пятнице")

			var asked []values.Question
			for _, c := range cards {
				if q, ok := c.(values.QuestionsCard); ok {
					asked = q.Questions
				}
			}
			if len(asked) != 1 || asked[0].Default != values.NoDefault || len(asked[0].Options) != 2 {
				t.Fatalf("questions = %+v", asked)
			}
			if st := e.sink.last(t); st.Status != thread.StatusAwaitingAnswers {
				t.Fatalf("update = %+v", st)
			}

			ref := agentturn.TurnRef{Thread: "th1", Turn: 1}
			for _, bad := range []int{values.NoDefault, values.MaxOptions} {
				if _, err := e.turns.AnswerQuestions(ctx, ref, []values.Answer{{Question: 0, Option: bad}}); !errors.Is(err, agentturn.ErrInvalidInput) {
					t.Fatalf("answer %d = %v, want invalid input", bad, err)
				}
			}

			args := `{"title":"Решить номер 5","due":"2026-10-23"}`
			if tt.subject != "" {
				args = `{"title":"Решить номер 5","subject":"` + string(e.math.ID()) + `","due":"2026-10-23"}`
			}
			e.hub.Then(testenv.AgentCall{ID: "c1", Tool: "create_assignment", Args: args}).
				Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Задание добавлено, срок 2026-10-23."}`})
			if err := e.driver.AnswerQuestions(ctx, ref, []values.Answer{{Question: 0, Option: tt.option}}); err != nil {
				t.Fatal(err)
			}
			e.driver.Wait()

			if got := e.plannerResults()["q1"]; got != fmt.Sprintf(`{"answers":[{"question":0,"option":%d}]}`, tt.option) {
				t.Fatalf("answers result = %s", got)
			}
			v, err := e.homework.Assignment(ctx, addedAssignmentID("th1", 1, "c1"))
			if err != nil || v.Title != "Решить номер 5" || v.Subject != tt.subject {
				t.Fatalf("assignment = %+v, %v", v, err)
			}
			if st := e.sink.last(t); st.Status != thread.StatusDone {
				t.Fatalf("last update = %+v", st)
			}
		})
	}
}

// The student edits the added assignment: its Undo is refused and the edit stays.
func TestAgentUndoOfAnEditedAssignmentIsRefused(t *testing.T) {
	e := newAssignmentEnv(t)
	ctx := t.Context()
	e.hub.Then(testenv.AgentCall{ID: "c1", Tool: "create_assignment", Args: `{"title":"Эссе","due":"2026-10-30"}`}).Then(endCall("e1"))
	e.ask(t, "запиши эссе к 30 октября")
	id := addedAssignmentID("th1", 1, "c1")
	v, err := e.homework.Assignment(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.homework.EditAssignment(ctx, homeworkflow.AssignmentEdit{
		ID: id, ExpectedVersion: v.Version, Title: "Эссе о Петре", Date: "2026-10-30",
	}); err != nil {
		t.Fatal(err)
	}
	entry, err := e.journal.Find(ctx, "th1", 1, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := e.journal.Undo(ctx, entry.ID); err != nil || u.Status != action.StatusUndoRefused || u.NoUndo != action.NoUndoChanged {
		t.Fatalf("Undo after the student's edit = %+v, %v", u, err)
	}
	if got, err := e.homework.Assignment(ctx, id); err != nil || got.Title != "Эссе о Петре" {
		t.Fatalf("assignment = %+v, %v", got, err)
	}
}

// The refusals the planner gets: a second create_assignment of the request needs a plan, a time without a due date is
// invalid, a subject the planner made up is unknown; none adds anything.
func TestAgentCreateAssignmentRefusals(t *testing.T) {
	e := newAssignmentEnv(t)
	ctx := t.Context()
	e.hub.Then(
		testenv.AgentCall{ID: "c1", Tool: "create_assignment", Args: `{"title":"Эссе"}`},
		testenv.AgentCall{ID: "c2", Tool: "create_assignment", Args: `{"title":"Доклад"}`},
	).Then(endCall("e1"))
	e.ask(t, "запиши эссе и доклад")
	e.hub.Then(testenv.AgentCall{ID: "c3", Tool: "create_assignment", Args: `{"title":"Лаба","time":"09:00"}`}).Then(endCall("e2"))
	e.ask(t, "лаба в 9")
	e.hub.Then(testenv.AgentCall{ID: "c4", Tool: "create_assignment", Args: `{"title":"Лаба","subject":"s-made-up"}`}).Then(endCall("e3"))
	e.ask(t, "лаба по физике")

	results := e.plannerResults()
	for id, want := range map[string]values.Refusal{
		"c2": values.RefusePlanRequired, "c3": values.RefuseInvalidArgs, "c4": values.RefuseUnknownID,
	} {
		if results[id] != `{"error":"`+string(want)+`"}` {
			t.Errorf("%s: result %s, want %s", id, results[id], want)
		}
	}
	if list, err := e.homework.ListAssignments(ctx); err != nil || len(list) != 1 || list[0].Title != "Эссе" {
		t.Fatalf("assignments = %+v, %v", list, err)
	}
	for turn, id := range map[int]string{1: "c2", 2: "c3", 3: "c4"} {
		if _, err := e.journal.Find(ctx, "th1", turn, id); err == nil {
			t.Errorf("%s was journaled", id)
		}
	}
	if st := e.sink.last(t); st.Status != thread.StatusDone {
		t.Fatalf("last update = %+v", st)
	}
	if strings.Contains(fmt.Sprint(results), "Доклад") {
		t.Fatal("a title reached the planner")
	}
}
