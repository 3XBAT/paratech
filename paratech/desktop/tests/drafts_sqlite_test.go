package tests

// Scenario tests of the homework drafts (TASK-043, PRD sections 10 and 14 case 5, AC-13) on the real stack: SQLite, the
// file store, the job runner and the homeworkflow use cases. The assignment comes from the post-processing of a
// transcript, as in the app. Only the outside world is fake: the clock, the STT engine and the LLM (testenv.FakeLLM).
// The draft repository is the in-memory testenv.MemDrafts until the sqlite one exists.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var _ jobrunner.Handler = (*homeworkflow.Drafts)(nil)

// draftIDs generates the ids of the draft use case, apart from those of the post-processing.
type draftIDs struct {
	mu sync.Mutex
	n  int
}

func (g *draftIDs) next(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return fmt.Sprintf("%s-%d", prefix, g.n)
}

func (g *draftIDs) NewAssignmentID() shared.AssignmentID {
	return shared.AssignmentID(g.next("draft-asg"))
}
func (g *draftIDs) NewJobID() shared.JobID     { return shared.JobID(g.next("draft-job")) }
func (g *draftIDs) NewProposalID() proposal.ID { return proposal.ID(g.next("draft-prop")) }

// newDraftsEnv is the post-processing stack of newPPEnv with the draft use case; register adds its job handler to the
// runner (before start).
func newDraftsEnv(t *testing.T, llm *testenv.FakeLLM, register bool) (*ppEnv, *homeworkflow.Drafts) {
	t.Helper()
	e := newPPEnv(t, llm)
	drafts, err := homeworkflow.NewDrafts(homeworkflow.DraftDeps{
		LLM: llm, Assignments: e.assignments, Drafts: testenv.NewMemDrafts(), Summaries: e.summaries,
		Lessons: sqlite.NewLessonRepository(e.db), Artifacts: e.store, Jobs: e.jobs, Clock: e.clock, IDs: &draftIDs{},
		Tx: e.db, ModelAlias: "default", MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if register {
		reg := jobrunner.Registration{Type: homeworkflow.HomeworkDraftJobType, Handler: drafts, Recovery: jobrunner.RecoveryRequeue}
		if err := e.runner.Register(reg); err != nil {
			t.Fatal(err)
		}
	}
	return e, drafts
}

// firstAssignment runs the post-processing and returns the first extracted assignment.
func firstAssignment(e *ppEnv) shared.AssignmentID {
	e.t.Helper()
	e.start()
	e.waitPost(homeworkflow.SummarizeJobType, job.StatusCompleted)
	e.waitPost(homeworkflow.ExtractHomeworkJobType, job.StatusCompleted)
	list := e.storedAssignments()
	if len(list) == 0 {
		e.t.Fatal("no assignment extracted")
	}
	return list[0].ID()
}

// waitJob drives the clock until job id is in status s.
func waitJob(e *ppEnv, id shared.JobID, s job.Status) *job.Job {
	e.t.Helper()
	var found *job.Job
	testenv.Eventually(e.t, fmt.Sprintf("job %s %s", id, s), func() bool {
		j, err := e.jobs.Get(context.Background(), id)
		found = j
		return err == nil && j.Status() == s
	}, func() { e.clock.Advance(time.Second) })
	return found
}

// Test steps 2 and 4 of TASK-043 (AC-13, PRD section 10): generate, edit locally, regenerate with additional
// instructions, export the final version to a file in the exports directory.
func TestDraftGenerateEditRegenerateExport(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, ppSummary)
	llm.Answer(values.OperationExtractHomework, ppExtract)
	llm.Answer(values.OperationHomeworkDraft, `{"title":"Chapter 4","body":"Deltas form where rivers reach the sea."}`)
	e, drafts := newDraftsEnv(t, llm, true)
	ctx := context.Background()
	a := firstAssignment(e)

	first, err := drafts.Generate(ctx, a, "")
	if err != nil || !first.Created {
		t.Fatalf("Generate: %+v, %v", first, err)
	}
	waitJob(e, first.JobID, job.StatusCompleted)

	if _, err := drafts.EditDraft(ctx, homeworkflow.DraftEdit{
		Assignment: a, Base: 1, Title: "Chapter 4", Body: "Deltas form where rivers reach the sea. Example: the Volga.",
	}); err != nil {
		t.Fatal(err)
	}

	llm.Answer(values.OperationHomeworkDraft, `{"title":"Chapter 4","body":"Deltas, with the Volga as the example."}`)
	second, err := drafts.Generate(ctx, a, "Shorter, keep the Volga")
	if err != nil || !second.Created || second.Base != 2 {
		t.Fatalf("regenerate: %+v, %v", second, err)
	}
	waitJob(e, second.JobID, job.StatusCompleted)

	list, err := drafts.DraftVersions(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	origins := []homeworkdraft.Origin{homeworkdraft.OriginGenerated, homeworkdraft.OriginEdited, homeworkdraft.OriginGenerated}
	if len(list) != len(origins) {
		t.Fatalf("versions = %d, want %d", len(list), len(origins))
	}
	for i, o := range origins {
		if list[i].Number != i+1 || list[i].Origin != o {
			t.Fatalf("version %d = %+v", i+1, list[i])
		}
	}
	if list[2].Instruction != "Shorter, keep the Volga" || list[2].Body != "Deltas, with the Volga as the example." {
		t.Fatalf("regenerated version = %+v", list[2])
	}

	reqs := llm.RequestsFor(values.OperationHomeworkDraft)
	if len(reqs) != 2 {
		t.Fatalf("draft requests = %d, want 2", len(reqs))
	}
	data := reqs[1].UntrustedData()
	if !strings.Contains(data, `"student_request":"Shorter, keep the Volga"`) || !strings.Contains(data, "Example: the Volga.") ||
		!strings.Contains(data, `"lesson_summary":"Rivers and their deltas."`) || !strings.Contains(data, `"lesson_title":"Geography"`) {
		t.Fatalf("regeneration request = %s", data)
	}

	exp, err := drafts.ExportDraft(ctx, a, 3)
	if err != nil {
		t.Fatal(err)
	}
	path, err := e.store.Path(ctx, exp.File)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/exports/" + string(a) + "/homework.md"; !strings.HasSuffix(filepath.ToSlash(path), want) {
		t.Fatalf("export path = %s, want it to end with %s", path, want)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("export file: %v", err)
	}
	if string(content) != "# Chapter 4\n\nDeltas, with the Volga as the example.\n" {
		t.Fatalf("export file = %q", content)
	}
}

// Test step 3 of TASK-043 (PRD section 14 case 5): concurrent Generate clicks with the same input make one job in
// the database. The draft handler is not registered, so the job stays queued and cannot move the base meanwhile.
func TestConcurrentGenerateMakesOneJob(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, ppSummary)
	llm.Answer(values.OperationExtractHomework, ppExtract)
	e, drafts := newDraftsEnv(t, llm, false)
	a := firstAssignment(e)

	const clicks = 6
	results := make([]homeworkflow.GenerateResult, clicks)
	errs := make([]error, clicks)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range clicks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = drafts.Generate(context.Background(), a, "Use simple words")
		}()
	}
	close(start)
	wg.Wait()

	created := 0
	for i := range clicks {
		if errs[i] != nil {
			t.Fatalf("click %d: %v", i, errs[i])
		}
		if results[i].JobID != results[0].JobID {
			t.Fatalf("click %d got job %s, click 0 got %s", i, results[i].JobID, results[0].JobID)
		}
		if results[i].Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created = %d, want 1", created)
	}

	jobs, err := e.jobs.ListByStatus(context.Background(), job.Statuses()...)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, j := range jobs {
		if j.Type() == homeworkflow.HomeworkDraftJobType {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("homework_draft jobs in the database = %d, want 1", n)
	}
}
