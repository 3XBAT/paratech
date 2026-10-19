package tests

// Scenario tests of the summary viewer (TASK-087) on the real stack of the post-processing (newPPEnv): SQLite, the
// file store, the job runner, the transcribe and homeworkflow use cases and the text file writer. Only the clock, the
// STT engine and the LLM are fake. The viewer reads what the jobs stored; it never calls the LLM.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/textfile"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/summaryview"
	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/testenv"
)

// The adapters satisfy the ports of the summary viewer.
var (
	_ summaryview.RecordingReader   = (*sqlite.RecordingRepository)(nil)
	_ summaryview.LessonReader      = (*sqlite.LessonRepository)(nil)
	_ summaryview.TranscriptFinder  = (*sqlite.TranscriptRepository)(nil)
	_ summaryview.SummaryReader     = (*sqlite.SummaryRepository)(nil)
	_ summaryview.LectureTextReader = (*sqlite.LectureTextRepository)(nil)
	_ summaryview.AssignmentLister  = (*sqlite.AssignmentRepository)(nil)
	_ summaryview.JobFinder         = (*sqlite.JobRepository)(nil)
	_ summaryview.TextWriter        = textfile.Writer{}
)

func newSummaryViewer(t *testing.T, e *ppEnv) *summaryview.Service {
	t.Helper()
	v, err := summaryview.New(summaryview.Deps{
		Recordings: sqlite.NewRecordingRepository(e.db), Lessons: sqlite.NewLessonRepository(e.db),
		Transcripts: sqlite.NewTranscriptRepository(e.db), Summaries: e.summaries,
		LectureTexts: sqlite.NewLectureTextRepository(e.db), Assignments: e.assignments, Jobs: e.jobs,
		Writer: textfile.New(), Clock: e.clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The summary, its key points and both deadlines are shown once the jobs completed, and saved as Markdown; the job
// keys of homeworkflow are found by the viewer's key prefix.
func TestSummaryViewerAfterPostProcessing(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, ppSummary)
	llm.Answer(values.OperationExtractHomework, ppExtract)
	e := newPPEnv(t, llm)
	e.start()
	e.waitPost(homeworkflow.SummarizeJobType, job.StatusCompleted)
	e.waitPost(homeworkflow.ExtractHomeworkJobType, job.StatusCompleted)
	requests := len(llm.Requests())

	viewer := newSummaryViewer(t, e)
	ctx := context.Background()
	v, err := viewer.View(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "Geography" || v.Lesson != "l1" || !v.Transcribed || v.Duration != 85*time.Minute {
		t.Fatalf("head = %+v", v)
	}
	if v.Summary.State != summaryview.StateDone || !v.Summary.Ready || v.Summary.Content.Text() != "Rivers and their deltas." {
		t.Fatalf("summary = %+v", v.Summary)
	}
	if v.Homework.State != summaryview.StateDone || len(v.Homework.Assignments) != 2 ||
		v.Homework.Assignments[0].Title != "Read chapter 4" ||
		!v.Homework.Assignments[0].Due.Equal(time.Date(2026, 10, 16, 20, 59, 0, 0, time.UTC)) {
		t.Fatalf("homework = %+v", v.Homework)
	}

	out := filepath.Join(t.TempDir(), "Конспект.md")
	if err := viewer.Export(ctx, "r1", summaryexport.FormatMarkdown, out); err != nil {
		t.Fatalf("Export: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Geography\n\n14\\.10.2026, 10:01 · 1 ч 25 мин\n\nRivers and their deltas.\n" +
		"\n## Ключевые моменты\n\n- Rivers flow to the sea\n" +
		"\n## Домашние задания\n\n- Read chapter 4 — до 16.10.2026, 23:59\n  Pages 40-52\n" +
		"- Draw a map of rivers — до 21.10.2026, 10:00\n"
	if string(data) != want {
		t.Fatalf("exported\n%q\nwant\n%q", data, want)
	}
	if n := len(llm.Requests()); n != requests {
		t.Fatalf("the viewer called the LLM: %d requests, want %d", n, requests)
	}
}

// An invalid answer fails both jobs: the viewer shows the failures with the core's fixed error, no model text.
func TestSummaryViewerShowsFailedPostProcessing(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, `Sure! Here is the summary: rivers.`)
	llm.Answer(values.OperationExtractHomework, `not json`)
	e := newPPEnv(t, llm)
	e.start()
	e.waitPost(homeworkflow.SummarizeJobType, job.StatusFailed)
	e.waitPost(homeworkflow.ExtractHomeworkJobType, job.StatusFailed)

	v, err := newSummaryViewer(t, e).View(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	for name, st := range map[string]summaryview.Step{"summary": v.Summary.Step, "homework": v.Homework.Step} {
		if st.State != summaryview.StateFailed || !strings.HasSuffix(st.JobError, homeworkflow.ErrInvalidAnswer.Error()) ||
			strings.Contains(st.JobError, "rivers") {
			t.Fatalf("%s = %+v", name, st)
		}
	}
	if v.Summary.Ready || len(v.Homework.Assignments) != 0 {
		t.Fatalf("results after failed jobs: %+v", v)
	}
}
