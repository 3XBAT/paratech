package tests

// Scenario tests of the agent's reversible actions (TASK-123, ADR-0021) on the real SQLite, the real library and the
// real journal and step use case; only the Hub is a fake. A folder and a subject are made by the planner's calls,
// journaled, undone (or refused because the student changed the subject since), and a crash between the use case and
// the journal's second transaction is settled at the next start.

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
	agentloop "paratech/desktop/internal/input/agent"
	"paratech/desktop/testenv"
)

var errNoUseCase = errors.New("not used in this scenario")

// noUseCase stands for the use cases this scenario does not reach (lessons, assignments, jobs).
type noUseCase struct{}

func (noUseCase) Lesson(context.Context, shared.RecordingID) (kb.Lesson, error) {
	return kb.Lesson{}, errNoUseCase
}
func (noUseCase) SetLessonInfo(context.Context, shared.RecordingID, string, string) (kb.Lesson, error) {
	return kb.Lesson{}, errNoUseCase
}
func (noUseCase) Assignment(context.Context, shared.AssignmentID) (homeworkflow.AssignmentView, error) {
	return homeworkflow.AssignmentView{}, errNoUseCase
}
func (noUseCase) EditAssignment(context.Context, homeworkflow.AssignmentEdit) (homeworkflow.AssignmentView, error) {
	return homeworkflow.AssignmentView{}, errNoUseCase
}
func (noUseCase) Retry(context.Context, shared.RecordingID, homeworkflow.Work) (shared.JobID, error) {
	return "", errNoUseCase
}
func (noUseCase) RedoSummary(context.Context, shared.RecordingID) (shared.JobID, error) {
	return "", errNoUseCase
}
func (noUseCase) ActiveGeneration(context.Context, shared.AssignmentID) (homeworkflow.GenerationView, bool, error) {
	return homeworkflow.GenerationView{}, false, errNoUseCase
}
func (noUseCase) Generate(context.Context, shared.AssignmentID, string) (homeworkflow.GenerateResult, error) {
	return homeworkflow.GenerateResult{}, errNoUseCase
}
func (noUseCase) ListRecordings(context.Context) ([]activity.Recording, error) { return nil, nil }
func (noUseCase) JobStates(context.Context, []shared.JobID) ([]activity.JobState, error) {
	return nil, errNoUseCase
}
func (noUseCase) Cancel(context.Context, shared.JobID) error { return errNoUseCase }

type actionsEnv struct {
	*agentEnv
	lib     *library.Service
	actions *agentloop.Actions
	driver  *agentloop.Driver
	sink    *driverSink
	physics *subject.Subject
}

func newActionsEnv(t *testing.T) *actionsEnv {
	t.Helper()
	e := &actionsEnv{agentEnv: newAgentEnv(t), sink: &driverSink{}}
	store, err := filestore.New(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if e.lib, err = library.New(library.Deps{
		Subjects: sqlite.NewSubjectRepository(e.db), Materials: sqlite.NewMaterialRepository(e.db), Blobs: store,
		Lessons: sqlite.NewLessonRepository(e.db), Strings: sqlite.NewSubjectStrings(e.db), Clock: e.clock, IDs: random.IDs{},
	}); err != nil {
		t.Fatal(err)
	}
	if e.physics, err = e.lib.CreateSubject(t.Context(), library.NewSubject{Name: "Физика"}); err != nil {
		t.Fatal(err)
	}
	e.restart(t)
	return e
}

// restart builds the use cases and the Driver on the stored state, as the app does at every start.
func (e *actionsEnv) restart(t *testing.T) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
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
	if e.actions, err = agentloop.NewActions(agentloop.ActionDeps{
		Library: e.lib, Lessons: noUseCase{}, Assignments: noUseCase{}, Rerunner: noUseCase{}, Drafts: noUseCase{},
		Jobs: noUseCase{}, Canceller: noUseCase{}, Tx: e.db, Clock: e.clock, Log: log,
	}); err != nil {
		t.Fatal(err)
	}
	if e.journal, err = agentjournal.New(agentjournal.Deps{
		Actions: sqlite.NewAgentActionRepository(e.db), Compensator: e.actions, Resolver: e.actions,
		Threads: sqlite.NewAgentThreadRepository(e.db), Processing: sqlite.NewJobRepository(e.db), Clock: e.clock,
		IDs: e.ids, Log: log,
	}); err != nil {
		t.Fatal(err)
	}
	if e.driver, err = agentloop.New(agentloop.Deps{
		Turns: e.turns, Sink: e.sink, Library: e.lib, Knowledge: &driverKnowledge{}, Activity: noUseCase{},
		Assignments: driverAssignments{}, Deliveries: driverDeliveries{}, Journal: e.journal, Actions: e.actions,
		Clock: e.clock, Log: log,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.driver.Close)
}

func (e *actionsEnv) ask(t *testing.T, scope values.Scope, text string) agentloop.Update {
	t.Helper()
	if err := e.driver.Ask(t.Context(), agentturn.AskInput{Scope: scope, Text: text}); err != nil {
		t.Fatal(err)
	}
	e.driver.Wait()
	return e.sink.last(t)
}

func (e *actionsEnv) subject(t *testing.T, id shared.SubjectID) (*subject.Subject, bool) {
	t.Helper()
	list, err := e.lib.Subjects(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.ID() == id {
			return s, true
		}
	}
	return nil, false
}

func folderNamed(s *subject.Subject, name string) (subject.Folder, bool) {
	for _, f := range s.Folders() {
		if f.Name().String() == name {
			return f, true
		}
	}
	return subject.Folder{}, false
}

// The planner makes a folder: it is journaled DONE with «Отменить»; the student adds a folder of their own, so the
// subject changed after the action and Undo is refused. A subject made in another request is undone with «Отменить
// всё», and «Журнал» lists both actions with the subject of their thread.
func TestAgentActionsJournalAndUndoOnSQLite(t *testing.T) {
	e := newActionsEnv(t)
	phys := string(e.physics.ID())
	scope, err := values.NewScope(agentRef(t, values.KindSubject, phys), values.ScreenSubjects, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	e.hub.Then(testenv.AgentCall{ID: "w1", Tool: "create_folder", Args: `{"subject":"` + phys + `","name":"Лабы"}`}).
		Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Папка создана"}`})
	if up := e.ask(t, scope, "создай папку Лабы"); up.Status != thread.StatusDone {
		t.Fatalf("update = %+v", up)
	}
	got, _ := e.subject(t, e.physics.ID())
	if _, ok := folderNamed(got, "Лабы"); !ok {
		t.Fatal("no folder")
	}
	if res := lastTurn(t, e.hub.Requests()[1]).GetResults().GetResults(); len(res) != 1 || res[0].GetIsError() {
		t.Fatalf("results = %v", res)
	}
	folderEntry, err := e.journal.Find(t.Context(), "th1", 1, "w1")
	if err != nil || folderEntry.Status != action.StatusDone || !folderEntry.CanUndo {
		t.Fatalf("entry = %+v, %v", folderEntry, err)
	}

	// The student edits the subject after the action: «Изменено после — отменить нельзя».
	if _, err := e.lib.CreateFolder(t.Context(), e.physics.ID(), "Мои заметки"); err != nil {
		t.Fatal(err)
	}
	if u, err := e.journal.Undo(t.Context(), folderEntry.ID); err != nil || u.Status != action.StatusUndoRefused || u.NoUndo != action.NoUndoChanged {
		t.Fatalf("Undo after the student's change = %+v, %v", u, err)
	}
	got, _ = e.subject(t, e.physics.ID())
	if _, ok := folderNamed(got, "Лабы"); !ok {
		t.Fatal("a refused Undo removed the folder")
	}

	e.hub.Then(testenv.AgentCall{ID: "w2", Tool: "create_subject", Args: `{"name":"Химия"}`}).
		Then(testenv.AgentCall{ID: "r2", Tool: "reply", Args: `{"text":"Предмет создан"}`})
	up := e.ask(t, values.Scope{}, "добавь химию")
	if up.Status != thread.StatusDone {
		t.Fatalf("update = %+v", up)
	}
	undone, err := e.journal.UndoRequest(t.Context(), up.Thread, up.Turn)
	if err != nil || len(undone) != 1 || undone[0].Status != action.StatusUndone {
		t.Fatalf("UndoRequest = %+v, %v", undone, err)
	}
	if list, _ := e.lib.Subjects(t.Context()); len(list) != 1 {
		t.Fatalf("subjects after Undo = %d, want only «Физика»", len(list))
	}

	lines, err := e.journal.List(t.Context(), 0)
	if err != nil || len(lines) != 2 {
		t.Fatalf("Journal = %+v, %v", lines, err)
	}
	if lines[0].Tool != values.ToolCreateSubject || lines[0].Status != action.StatusUndone || lines[0].Subject != "" ||
		lines[1].Tool != values.ToolCreateFolder || lines[1].NoUndo != action.NoUndoChanged || lines[1].Subject != e.physics.ID() {
		t.Fatalf("Journal lines = %+v", lines)
	}
}

// The app stops after the folder was made but before the journal recorded it: the entry stays PENDING with its intent
// in SQLite, and at the next start Recover finds the folder (DONE, with «Отменить» that works).
func TestAgentActionSettledAfterACrashOnSQLite(t *testing.T) {
	e := newActionsEnv(t)
	subj := agentRef(t, values.KindSubject, string(e.physics.ID()))
	scope, err := values.NewScope(subj, values.ScreenSubjects, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	e.hub.Then(testenv.AgentCall{ID: "r0", Tool: "reply", Args: `{"text":"ок"}`})
	up := e.ask(t, scope, "привет") // the thread of the subject exists
	before, _ := e.subject(t, e.physics.ID())
	entry, err := e.journal.Begin(t.Context(), agentjournal.BeginInput{
		Thread: up.Thread, Turn: up.Turn, CallID: "w1", Tool: values.ToolCreateFolder,
		Intent: action.Intent{Target: subj, Before: before.Version(), Name: "Проекты"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.CreateFolder(t.Context(), e.physics.ID(), "Проекты"); err != nil {
		t.Fatal(err)
	}

	// Next start.
	e.restart(t)
	rep, err := e.journal.Recover(t.Context())
	if err != nil || rep != (agentjournal.RecoverReport{Done: 1}) {
		t.Fatalf("Recover = %+v, %v", rep, err)
	}
	if u, err := e.journal.Undo(t.Context(), entry.ID); err != nil || u.Status != action.StatusUndone {
		t.Fatalf("Undo of the settled entry = %+v, %v", u, err)
	}
	after, _ := e.subject(t, e.physics.ID())
	if _, ok := folderNamed(after, "Проекты"); ok {
		t.Fatal("the folder is still there")
	}
}
