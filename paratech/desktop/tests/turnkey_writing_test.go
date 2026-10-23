package tests

// The turnkey writing (TASK-131, ADR-0025 addendum) end to end: the agent's write_work through the Driver on an
// approved plan, the writing job on the real job runner and SQLite (the outline, one section per step, the .docx by the
// real renderer), the paper filed into the subject's «ДЗ» of the real library; only the Hub is a fake. The injection
// fixtures run as the assignment text and as the writer's answer: they end up as paper text only.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// noLessonLabels stands for cases/homeworkflow: a work made from an uploaded file has no assignment lesson.
type noLessonLabels struct{}

func (noLessonLabels) LessonLabel(context.Context, shared.AssignmentID) (cvalues.LessonLabel, error) {
	return cvalues.LessonLabel{}, shared.ErrNotFound
}

const writingOutline = `{"topic":"Древняя Русь","sections":[{"title":"Введение","words":200},{"title":"Заключение","words":150}]}`

func writingSection(text string) string {
	q, _ := json.Marshal(text)
	return `{"paragraphs":[` + string(q) + `],"sources":[{"material":"m1","text":""},{"material":"","text":` + string(q) + `}]}`
}

// startRunner runs the writing jobs on the real job runner.
func (e *turnkeyEnv) startRunner(t *testing.T) *jobrunner.Service {
	t.Helper()
	runner, err := jobrunner.New(jobrunner.Deps{Jobs: e.jobs, Clock: e.clock, Sleeper: e.clock}, runnerConfig(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Register(jobrunner.Registration{
		Type: turnkey.WritingJobType, Handler: e.works, Recovery: jobrunner.RecoveryRequeue,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := runner.Stop(stop); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return runner
}

// confirmedByAgent triages the upload through the agent and confirms the requirements as the student does.
func (e *turnkeyEnv) confirmedByAgent(t *testing.T) values.Ref {
	t.Helper()
	e.reader.AnswerTriage(triageOutput("", 0, work.FeasibleFull))
	wc := workCardOf(t, e.runTriage(t))
	if v, err := e.binding.ConfirmRequirements(t.Context(), wc.Work.ID()); err != nil || v.Status != "confirmed" {
		t.Fatalf("confirm = %+v, %v", v, err)
	}
	return wc.Work
}

// fileUnder files the upload under a new subject, so the paper goes to that subject's «ДЗ».
func (e *turnkeyEnv) fileUnder(t *testing.T, name string) *subject.Subject {
	t.Helper()
	sub, err := e.lib.CreateSubject(t.Context(), library.NewSubject{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Place(t.Context(), shared.MaterialID(e.upload.ID()), sub.ID(), ""); err != nil {
		t.Fatal(err)
	}
	return sub
}

// runWriteWork asks the agent to write the work: a plan with write_work, «Да», the call and a reply.
func (e *turnkeyEnv) runWriteWork(t *testing.T, w values.Ref) {
	t.Helper()
	ctx := t.Context()
	step, _ := json.Marshal(map[string]any{
		"summary": "Написать работу", "steps": []any{map[string]any{"tool": "write_work", "arguments": map[string]any{"work": w.ID()}}},
	})
	e.hub.Then(testenv.AgentCall{ID: "p2", Tool: "show_plan", Args: string(step)}).
		Then(testenv.AgentCall{ID: "ww1", Tool: "write_work", Args: `{"work":"` + w.ID() + `"}`}).
		Then(testenv.AgentCall{ID: "e2", Tool: "reply", Args: `{"text":"Работа пишется"}`})
	if err := e.driver.Ask(ctx, agentturn.AskInput{Scope: values.Scope{}, Text: "напиши работу"}); err != nil {
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
}

// waitWritten waits until the work is written (or its job ended without it).
func (e *turnkeyEnv) waitWritten(t *testing.T, id shared.WorkID, jobID shared.JobID) turnkey.View {
	t.Helper()
	var v turnkey.View
	testenv.Eventually(t, fmt.Sprintf("work %s written", id), func() bool {
		// The job first: a work read after its job ended shows what the job left.
		j, jerr := e.jobs.Get(context.Background(), jobID)
		var err error
		v, err = e.works.Work(context.Background(), id)
		return err == nil && jerr == nil && (v.Status == work.StatusDone || j.Status().IsTerminal())
	}, func() { e.clock.Advance(time.Second) })
	if v.Status != work.StatusDone {
		j, _ := e.jobs.Get(context.Background(), jobID)
		t.Fatalf("work = %s, job %s (%s)", v.Status, j.Status(), j.LastError())
	}
	return v
}

// writerRequests are the writing calls the Hub got, with the number of distinct keys.
func (e *turnkeyEnv) writerRequests() ([]*hubv1.CompletionSpec, int) {
	var out []*hubv1.CompletionSpec
	keys := map[string]bool{}
	for _, r := range e.reader.Requests() {
		if r.GetSpec().GetOperationType() == hubv1.OperationType_OPERATION_TYPE_HOMEWORK_DRAFT {
			out = append(out, r.GetSpec())
			keys[r.GetSpec().GetIdempotencyKey()] = true
		}
	}
	return out, len(keys)
}

// jobOfCall is the job a write_work call started, from its journal entry.
func (e *turnkeyEnv) jobOfCall(t *testing.T, callID string) shared.JobID {
	t.Helper()
	entry, err := e.journal.Find(t.Context(), "th1", 2, callID)
	if err != nil || entry.Status != action.StatusDone {
		t.Fatalf("journal = %+v, %v", entry, err)
	}
	stop, ok := entry.Compensation.(action.StopWork)
	if !ok {
		t.Fatalf("compensation = %+v", entry.Compensation)
	}
	return shared.JobID(stop.Job.ID())
}

// TestTurnkeyWriteWorkEndToEnd: the agent writes a confirmed work on an approved plan; the job writes the outline and
// one section per step, files the .docx into «ДЗ» of the subject; each section is asked once; the planner saw the job
// only.
func TestTurnkeyWriteWorkEndToEnd(t *testing.T) {
	e := newTurnkeyEnv(t, "Реферат по истории Древней Руси: введение и заключение.")
	ctx := t.Context()
	w := e.confirmedByAgent(t)
	sub := e.fileUnder(t, "История")
	e.reader.AnswerInOrder(cvalues.OperationHomeworkDraft, writingOutline, writingSection("Текст раздела."))
	e.startRunner(t)

	e.runWriteWork(t, w)
	r, ok := resultOf(e.agentEnv, "ww1")
	if !ok || r.GetIsError() || !strings.Contains(r.GetResultJson(), `"job":`) || strings.Contains(r.GetResultJson(), "Русь") {
		t.Fatalf("write_work result = %v", r)
	}
	jobID := e.jobOfCall(t, "ww1")
	v := e.waitWritten(t, shared.WorkID(w.ID()), jobID)

	// The paper is in «ДЗ» of the subject, a Word file with the mark, the sections and the marked source.
	if v.Writing.Done != 2 || v.Writing.Total != 2 || v.Writing.Result.IsZero() || v.Writing.Subject != sub.ID() {
		t.Fatalf("writing = %+v", v.Writing)
	}
	m, text, err := e.lib.MaterialText(ctx, v.Writing.Material)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type() != material.TypeDOCX || m.Subject() != sub.ID() || m.Folder() != subject.SystemFolderID(sub.ID(), subject.SystemHomework) {
		t.Fatalf("paper = %+v", m.State())
	}
	for _, want := range []string{homeworkdoc.GeneratedMark, "Введение", "Текст раздела.", strings.TrimSpace(homeworkdoc.VerifySource),
		cvalues.BibliographyHeading, "Древняя Русь"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("the paper has no %q", want)
		}
	}
	if j, err := e.jobs.Get(ctx, jobID); err != nil || j.Status() != job.StatusCompleted || j.Snapshot().Checkpoint() != "2/2" {
		t.Fatalf("job = %+v, %v", j, err)
	}

	// The outline and two sections: three calls, three keys; the instructions hold no text of the assignment.
	specs, keys := e.writerRequests()
	if len(specs) != 3 || keys != 3 {
		t.Fatalf("writer calls %d, keys %d", len(specs), keys)
	}
	for _, s := range specs {
		if strings.Contains(s.GetInstructions(), "Древней") || s.GetModelAlias() != "default" {
			t.Fatalf("writer request = %v", s)
		}
	}
	// The planner never saw the paper.
	for _, req := range e.hub.Requests() {
		raw, _ := json.Marshal(req.GetTurns())
		if strings.Contains(string(raw), "Текст раздела") || strings.Contains(string(raw), "Древняя Русь") {
			t.Fatal("the writer's text reached the planner")
		}
	}

	// Done: write_work again in a new request is no action.
	if _, err := e.works.StartWriting(ctx, shared.WorkID(w.ID())); err == nil {
		t.Fatal("a done work started writing again")
	}
}

// TestTurnkeyWritingCancelledAndResumed: a cancelled writing job leaves the stored sections; the next start is a new
// job that asks no stored step again.
func TestTurnkeyWritingCancelledAndResumed(t *testing.T) {
	e := newTurnkeyEnv(t, "Эссе о Пушкине.")
	ctx := t.Context()
	e.reader.AnswerTriage(triageOutput("", 0, work.FeasibleFull))
	st, err := e.works.Triage(ctx, work.Source{Material: shared.MaterialID(e.upload.ID())})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.works.ConfirmRequirements(ctx, st.View.ID); err != nil {
		t.Fatal(err)
	}
	e.reader.AnswerInOrder(cvalues.OperationHomeworkDraft, writingOutline, writingSection("Абзац."))

	first, err := e.works.StartWriting(ctx, st.View.ID)
	if err != nil || !first.Created {
		t.Fatalf("StartWriting = %+v, %v", first, err)
	}
	// One step by hand (the outline is stored), then the job is cancelled before the runner takes it.
	j, err := e.jobs.Get(ctx, first.Job)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.works.Handle(ctx, j.Snapshot(), nil); err == nil {
		t.Fatal("the outline step completed the job")
	}
	runner := e.startRunnerStopped(t)
	if err := runner.Cancel(ctx, first.Job); err != nil {
		t.Fatal(err)
	}

	again, err := e.works.StartWriting(ctx, st.View.ID)
	if err != nil || !again.Created || again.Job == first.Job {
		t.Fatalf("StartWriting after the cancel = %+v, %v", again, err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	v := e.waitWritten(t, st.View.ID, again.Job)
	if v.Writing.Material == "" || v.Writing.Result.IsZero() || v.Writing.Done != 2 {
		t.Fatalf("writing = %+v", v.Writing)
	}
	// The paper of an upload in «Входящие» stays in «Входящие».
	m, err := e.lib.Material(ctx, v.Writing.Material)
	if err != nil || !m.InInbox() {
		t.Fatalf("paper = %+v, %v", m, err)
	}
	if specs, keys := e.writerRequests(); len(specs) != 3 || keys != 3 {
		t.Fatalf("writer calls %d, keys %d: a stored step was asked again", len(specs), keys)
	}
}

// startRunnerStopped builds the runner of the writing jobs without starting it.
func (e *turnkeyEnv) startRunnerStopped(t *testing.T) *jobrunner.Service {
	t.Helper()
	runner, err := jobrunner.New(jobrunner.Deps{Jobs: e.jobs, Clock: e.clock, Sleeper: e.clock}, runnerConfig(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Register(jobrunner.Registration{
		Type: turnkey.WritingJobType, Handler: e.works, Recovery: jobrunner.RecoveryRequeue,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = runner.Stop(stop)
	})
	return runner
}

// TestTurnkeyWritingJobOfAnAssignmentIsFound: the job of a work made from an assignment names it, so deleting the
// assignment with its recording finds and cancels the job (recordingdelete, TASK-095).
func TestTurnkeyWritingJobOfAnAssignmentIsFound(t *testing.T) {
	e := newTurnkeyEnv(t, "x")
	ctx := t.Context()
	payload, err := cvalues.NewWorkJob("w-a", "a-1")
	if err != nil {
		t.Fatal(err)
	}
	j, err := job.New(job.Spec{ID: "j-write", Type: turnkey.WritingJobType, Key: "work_writing:w-a:g1", Payload: payload.Payload(),
		MaxAttempts: 3}, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.jobs.Enqueue(ctx, j); err != nil {
		t.Fatal(err)
	}
	got, bad, err := e.jobs.ListForAssignments(ctx, []shared.AssignmentID{"a-1"}, []job.Type{turnkey.WritingJobType})
	if err != nil || len(bad) != 0 || len(got) != 1 || got[0].ID() != "j-write" {
		t.Fatalf("ListForAssignments = %v, %v, %v", got, bad, err)
	}
}

// TestTurnkeyWritingInjectionStaysPaperText: a command in the assignment and in the writer's answer (injection
// fixtures) ends up as text of the paper only: one paper is filed, nothing else is journaled or done, and the planner
// never sees it.
func TestTurnkeyWritingInjectionStaysPaperText(t *testing.T) {
	for _, fx := range loadInjectionFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			e := newTurnkeyEnv(t, "Задание: реферат.\n"+fx.Untrusted)
			ctx := t.Context()
			w := e.confirmedByAgent(t)
			sub := e.fileUnder(t, "История")
			line := strings.Join(strings.Fields(fx.Untrusted), " ")
			e.reader.AnswerInOrder(cvalues.OperationHomeworkDraft, writingOutline, writingSection(line))
			e.startRunner(t)
			e.runWriteWork(t, w)
			v := e.waitWritten(t, shared.WorkID(w.ID()), e.jobOfCall(t, "ww1"))

			_, text, err := e.lib.MaterialText(ctx, v.Writing.Material)
			if err != nil || !strings.Contains(text.String(), line) {
				t.Fatalf("the paper text: %v", err)
			}
			files, err := e.lib.Materials(ctx, sub.ID())
			if err != nil || len(files) != 2 { // the upload and the paper
				t.Fatalf("files of the subject = %d, %v", len(files), err)
			}
			list, err := e.journal.ListThread(ctx, "th1", 0)
			if err != nil || len(list) != 2 {
				t.Fatalf("journal = %+v, %v", list, err)
			}
			for _, req := range e.hub.Requests() {
				raw, _ := json.Marshal(req.GetTurns())
				if strings.Contains(string(raw), line) {
					t.Fatal("the untrusted text reached the planner")
				}
			}
			specs, _ := e.writerRequests()
			for _, s := range specs {
				if strings.Contains(s.GetInstructions(), line) {
					t.Fatal("the untrusted text reached the writer's instructions")
				}
			}
		})
	}
}
