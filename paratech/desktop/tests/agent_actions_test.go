package tests

// Scenario tests of the agent's reversible actions (TASK-123, ADR-0021) on the real SQLite, the real library and the
// real journal and step use case; only the Hub is a fake. A folder and a subject are made by the planner's calls,
// journaled, undone (or refused because the student changed the subject since), «Отменить всё» undoes two actions of a
// request on the same target, and a crash between the use case and the journal's second transaction is settled at the
// next start.

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/doctext"
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
	"paratech/desktop/internal/domain/library/inspection"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
	agentloop "paratech/desktop/internal/input/agent"
	"paratech/desktop/testenv"
)

var errNoUseCase = errors.New("not used in this scenario")

// lectureAnswer is a reader answer inside the schema of INSPECT_MATERIAL.
const lectureAnswer = `{"doc_kind":"lecture","subject":null,"lesson_date":null,"has_homework":false,` +
	`"has_ticket_list":false,"mentions_email":false}`

// fixedReader stands for the quarantined reader on the Hub: it answers every request with the same text.
type fixedReader string

func (r fixedReader) InspectMaterial(context.Context, inspection.Request) (string, error) {
	return string(r), nil
}

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
func (noUseCase) SetAssignmentDue(context.Context, shared.AssignmentID, int64, time.Time) (homeworkflow.AssignmentView, error) {
	return homeworkflow.AssignmentView{}, errNoUseCase
}
func (noUseCase) Retry(context.Context, shared.RecordingID, homeworkflow.Work) (shared.JobID, bool, error) {
	return "", false, errNoUseCase
}
func (noUseCase) RedoSummary(context.Context, shared.RecordingID) (shared.JobID, bool, error) {
	return "", false, errNoUseCase
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

// actionsActivity lists the recordings (with their schedule lessons) find_lessons maps; the rest is not used.
type actionsActivity struct {
	noUseCase
	recordings []activity.Recording
}

func (a actionsActivity) ListRecordings(context.Context) ([]activity.Recording, error) {
	return a.recordings, nil
}

type actionsEnv struct {
	*agentEnv
	knowledge *driverKnowledge
	activity  actionsActivity
	lib       *library.Service
	actions   *agentloop.Actions
	driver    *agentloop.Driver
	sink      *driverSink
	physics   *subject.Subject
}

func newActionsEnv(t *testing.T) *actionsEnv {
	t.Helper()
	e := &actionsEnv{agentEnv: newAgentEnv(t), sink: &driverSink{}, knowledge: &driverKnowledge{}}
	store, err := filestore.New(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if e.lib, err = library.New(library.Deps{
		Subjects: sqlite.NewSubjectRepository(e.db), Materials: sqlite.NewMaterialRepository(e.db), Blobs: store,
		Lessons: sqlite.NewLessonRepository(e.db), Strings: sqlite.NewSubjectStrings(e.db), Clock: e.clock, IDs: random.IDs{},
		Texts: doctext.New(), Reader: fixedReader(lectureAnswer), Inspections: sqlite.NewInspectionRepository(e.db),
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
		Turns: e.turns, Sink: e.sink, Library: e.lib, Inspector: e.lib, Knowledge: e.knowledge, Activity: e.activity,
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

// run waits for the Driver and returns its last update.
func (e *actionsEnv) run(t *testing.T, start func(ctx context.Context) error) agentloop.Update {
	t.Helper()
	if err := start(t.Context()); err != nil {
		t.Fatal(err)
	}
	e.driver.Wait()
	return e.sink.last(t)
}

// «Отменить всё» of a request that made a subject and then a folder in it: undoing the folder moves the subject's
// version, and the subject follows it, so both are UNDONE. A folder the student adds to the new subject between the
// two actions still refuses the subject («Изменено после»).
func TestAgentUndoRequestOfASubjectAndItsFolderOnSQLite(t *testing.T) {
	for _, studentEdits := range []bool{false, true} {
		t.Run(map[bool]string{false: "both undone", true: "student change in between"}[studentEdits], func(t *testing.T) {
			e := newActionsEnv(t)
			plan := desktopPlan("План: предмет и папка.",
				desktopStep("create_subject", `{"name":"Химия"}`), desktopStep("create_folder", `{"name":"Лабы"}`))
			e.hub.Then(testenv.AgentCall{ID: "p1", Tool: "show_plan", Args: plan}).
				Then(testenv.AgentCall{ID: "w1", Tool: "create_subject", Args: `{"name":"Химия"}`})
			up := e.ask(t, values.Scope{}, "добавь химию с папкой Лабы")
			if up.Status != thread.StatusAwaitingPlan {
				t.Fatalf("plan = %+v", up)
			}
			ref := agentturn.TurnRef{Thread: up.Thread, Turn: up.Turn}

			// The subject is made; the next planner step is not scripted yet, so the request stops here.
			if up := e.run(t, func(ctx context.Context) error { return e.driver.ApprovePlan(ctx, ref) }); up.StopReason != values.StopOffline {
				t.Fatalf("after «Да» = %+v, want stopped offline", up)
			}
			subj, err := e.journal.Find(t.Context(), up.Thread, up.Turn, "w1")
			if err != nil || subj.Status != action.StatusDone {
				t.Fatalf("subject entry = %+v, %v", subj, err)
			}
			chem := subj.Compensation.(action.RemoveSubject).Subject.ID()
			if studentEdits {
				if _, err := e.lib.AddAlias(t.Context(), shared.SubjectID(chem), "химия орг."); err != nil {
					t.Fatal(err)
				}
			}

			e.hub.Then(testenv.AgentCall{ID: "w2", Tool: "create_folder", Args: `{"subject":"` + chem + `","name":"Лабы"}`}).
				Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Готово"}`})
			if up := e.run(t, func(ctx context.Context) error { return e.driver.Continue(ctx, ref) }); up.Status != thread.StatusDone {
				t.Fatalf("after «Продолжить» = %+v", up)
			}
			if folder, err := e.journal.Find(t.Context(), up.Thread, up.Turn, "w2"); err != nil || folder.Status != action.StatusDone {
				t.Fatalf("folder entry = %+v, %v", folder, err)
			}

			undone, err := e.journal.UndoRequest(t.Context(), up.Thread, up.Turn)
			if err != nil || len(undone) != 2 || undone[0].Tool != values.ToolCreateFolder || undone[0].Status != action.StatusUndone {
				t.Fatalf("UndoRequest = %+v, %v", undone, err)
			}
			_, left := e.subject(t, shared.SubjectID(chem))
			if studentEdits {
				if undone[1].Status != action.StatusUndoRefused || undone[1].NoUndo != action.NoUndoChanged || !left {
					t.Fatalf("subject after the student's change = %+v, still there %v", undone[1], left)
				}
				return
			}
			if undone[1].Status != action.StatusUndone || left {
				t.Fatalf("subject = %+v, still there %v", undone[1], left)
			}
		})
	}
}

// «Отменить всё» of a request that filed an upload with a lesson and then linked it to another lesson: undoing the
// link moves the material's version, and the placing follows it, so the file is back in «Входящие» and unlinked. A
// move by the student between the two actions still refuses the placing.
func TestAgentUndoRequestOfAPlacedAndLinkedUploadOnSQLite(t *testing.T) {
	for _, studentMoves := range []bool{false, true} {
		t.Run(map[bool]string{false: "both undone", true: "student change in between"}[studentMoves], func(t *testing.T) {
			e := newActionsEnv(t)
			phys := string(e.physics.ID())
			lessons := sqlite.NewLessonRepository(e.db)
			saveScheduleLesson(t, lessons, "l1", "Механика", "Физика", libScenarioT0)
			saveScheduleLesson(t, lessons, "l2", "Оптика", "Физика", libScenarioT0.Add(24*time.Hour))
			// find_lessons shows lesson l2 (its recording r2), so the planner may name it.
			e.knowledge.lessons = []kb.Lesson{{Recording: "r2", StartedAt: libScenarioT0.Add(24 * time.Hour), Subject: "Физика",
				Kind: "lecture", Library: kb.LibrarySubject{ID: e.physics.ID(), Name: "Физика"}}}
			e.activity.recordings = []activity.Recording{{ID: "r2", LessonID: "l2"}}
			e.restart(t)

			imp, err := e.lib.Import(t.Context(), library.Upload{Name: "Лабораторная.docx", Data: libraryDocx(t)})
			if err != nil {
				t.Fatal(err)
			}
			m := string(imp.Material.ID())
			scope, err := values.NewScope(agentRef(t, values.KindSubject, phys), values.ScreenSubjects, agentRef(t, values.KindLesson, "l1"), values.Ref{})
			if err != nil {
				t.Fatal(err)
			}
			link := `{"material":"` + m + `","lesson":"l2"}`
			e.hub.Then(testenv.AgentCall{ID: "q1", Tool: "find_lessons", Args: `{}`}).
				Then(testenv.AgentCall{ID: "w1", Tool: "place_upload", Args: `{"upload":"` + m + `","subject":"` + phys + `","lesson":"l1"}`}).
				Then(testenv.AgentCall{ID: "p1", Tool: "show_plan", Args: desktopPlan("План: привязать к занятию.", desktopStep("link_material", link))}).
				Then(testenv.AgentCall{ID: "w2", Tool: "link_material", Args: link}).
				Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Готово"}`})
			up := e.run(t, func(ctx context.Context) error {
				return e.driver.Ask(ctx, agentturn.AskInput{Scope: scope, Text: "положи файл в физику и привяжи",
					Uploads: []values.Ref{agentRef(t, values.KindUpload, m)}})
			})
			if up.Status != thread.StatusAwaitingPlan {
				t.Fatalf("plan = %+v", up)
			}
			if studentMoves {
				if _, err := e.lib.Place(t.Context(), imp.Material.ID(), e.physics.ID(), subject.SystemFolderID(e.physics.ID(), subject.SystemLectures)); err != nil {
					t.Fatal(err)
				}
			}
			ref := agentturn.TurnRef{Thread: up.Thread, Turn: up.Turn}
			if up := e.run(t, func(ctx context.Context) error { return e.driver.ApprovePlan(ctx, ref) }); up.Status != thread.StatusDone {
				t.Fatalf("after «Да» = %+v", up)
			}

			undone, err := e.journal.UndoRequest(t.Context(), up.Thread, up.Turn)
			if err != nil || len(undone) != 2 || undone[0].Tool != values.ToolLinkMaterial || undone[0].Status != action.StatusUndone {
				t.Fatalf("UndoRequest = %+v, %v", undone, err)
			}
			after, err := e.lib.Material(t.Context(), imp.Material.ID())
			if err != nil || slices.Contains(after.Lessons(), "l2") {
				t.Fatalf("material = %+v, %v; want unlinked from l2", after, err)
			}
			if studentMoves {
				if undone[1].Status != action.StatusUndoRefused || undone[1].NoUndo != action.NoUndoChanged || after.InInbox() ||
					!slices.Equal(after.Lessons(), []shared.LessonID{"l1"}) {
					t.Fatalf("placing after the student's move = %+v, in «Входящие» %v, lessons %v", undone[1], after.InInbox(), after.Lessons())
				}
				return
			}
			if undone[1].Tool != values.ToolPlaceUpload || undone[1].Status != action.StatusUndone || !after.InInbox() || len(after.Lessons()) != 0 {
				t.Fatalf("placing = %+v, in «Входящие» %v, lessons %v", undone[1], after.InInbox(), after.Lessons())
			}
		})
	}
}

// Reading an upload (inspect_upload) records its text status but is not a change of the material (TASK-122 review): a
// placing or link before it is still undone, and a placing in the same step as the reading still runs.
func TestAgentInspectKeepsUndoOfAMaterialOnSQLite(t *testing.T) {
	tests := []struct {
		name  string
		steps func(m, phys, link string) [][]testenv.AgentCall
		undo  []values.Tool // the journal lines «Отменить всё» undoes, newest first
	}{
		{
			name: "place then inspect",
			steps: func(m, phys, _ string) [][]testenv.AgentCall {
				return [][]testenv.AgentCall{
					{{ID: "w1", Tool: "place_upload", Args: `{"upload":"` + m + `","subject":"` + phys + `","lesson":"l1"}`}},
					{{ID: "i1", Tool: "inspect_upload", Args: `{"upload":"` + m + `"}`}},
				}
			},
			undo: []values.Tool{values.ToolPlaceUpload},
		},
		{
			name: "place, link, then inspect",
			steps: func(m, phys, link string) [][]testenv.AgentCall {
				return [][]testenv.AgentCall{
					{{ID: "q1", Tool: "find_lessons", Args: `{}`}},
					{{ID: "w1", Tool: "place_upload", Args: `{"upload":"` + m + `","subject":"` + phys + `","lesson":"l1"}`}},
					{{ID: "p1", Tool: "show_plan", Args: desktopPlan("План: привязать к занятию.", desktopStep("link_material", link))}},
					{{ID: "w2", Tool: "link_material", Args: link}},
					{{ID: "i1", Tool: "inspect_upload", Args: `{"upload":"` + m + `"}`}},
				}
			},
			undo: []values.Tool{values.ToolLinkMaterial, values.ToolPlaceUpload},
		},
		{
			name: "inspect and place in one step",
			steps: func(m, phys, _ string) [][]testenv.AgentCall {
				return [][]testenv.AgentCall{{
					{ID: "i1", Tool: "inspect_upload", Args: `{"upload":"` + m + `"}`},
					{ID: "w1", Tool: "place_upload", Args: `{"upload":"` + m + `","subject":"` + phys + `","lesson":"l1"}`},
				}}
			},
			undo: []values.Tool{values.ToolPlaceUpload},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newActionsEnv(t)
			phys := string(e.physics.ID())
			lessons := sqlite.NewLessonRepository(e.db)
			saveScheduleLesson(t, lessons, "l1", "Механика", "Физика", libScenarioT0)
			saveScheduleLesson(t, lessons, "l2", "Оптика", "Физика", libScenarioT0.Add(24*time.Hour))
			e.knowledge.lessons = []kb.Lesson{{Recording: "r2", StartedAt: libScenarioT0.Add(24 * time.Hour), Subject: "Физика",
				Kind: "lecture", Library: kb.LibrarySubject{ID: e.physics.ID(), Name: "Физика"}}}
			e.activity.recordings = []activity.Recording{{ID: "r2", LessonID: "l2"}}
			e.restart(t)

			imp, err := e.lib.Import(t.Context(), library.Upload{Name: "Лабораторная.docx", Data: libraryDocx(t)})
			if err != nil {
				t.Fatal(err)
			}
			m := string(imp.Material.ID())
			scope, err := values.NewScope(agentRef(t, values.KindSubject, phys), values.ScreenSubjects, agentRef(t, values.KindLesson, "l1"), values.Ref{})
			if err != nil {
				t.Fatal(err)
			}
			for _, step := range tt.steps(m, phys, `{"material":"`+m+`","lesson":"l2"}`) {
				e.hub.Then(step...)
			}
			e.hub.Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Готово"}`})
			up := e.run(t, func(ctx context.Context) error {
				return e.driver.Ask(ctx, agentturn.AskInput{Scope: scope, Text: "посмотри файл и положи в физику",
					Uploads: []values.Ref{agentRef(t, values.KindUpload, m)}})
			})
			if up.Status == thread.StatusAwaitingPlan {
				ref := agentturn.TurnRef{Thread: up.Thread, Turn: up.Turn}
				up = e.run(t, func(ctx context.Context) error { return e.driver.ApprovePlan(ctx, ref) })
			}
			if up.Status != thread.StatusDone {
				t.Fatalf("update = %+v", up)
			}
			placed, err := e.lib.Material(t.Context(), imp.Material.ID())
			if err != nil || placed.InInbox() || placed.TextStatus() != material.TextExtracted {
				t.Fatalf("after the request: %+v, %v; want placed and read", placed.State(), err)
			}

			undone, err := e.journal.UndoRequest(t.Context(), up.Thread, up.Turn)
			if err != nil || len(undone) != len(tt.undo) {
				t.Fatalf("UndoRequest = %+v, %v", undone, err)
			}
			for i, tool := range tt.undo {
				if undone[i].Tool != tool || undone[i].Status != action.StatusUndone {
					t.Fatalf("undone[%d] = %+v, want %s undone", i, undone[i], tool)
				}
			}
			after, err := e.lib.Material(t.Context(), imp.Material.ID())
			if err != nil || !after.InInbox() || len(after.Lessons()) != 0 || after.TextStatus() != material.TextExtracted {
				t.Fatalf("after Undo: %+v, %v", after.State(), err)
			}
		})
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
