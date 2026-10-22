package tests

// Scenario tests of «Сегодня» (TASK-125) on the real stack of the post-processing (SQLite, file store, job runner,
// transcribe and homeworkflow) with the real todayfeed Poster and Screen over the sqlite adapters. Fake are the clock,
// the STT engine, the LLM and the two ports the app maps over other use cases (assignments, library subjects).

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/todayfeed"
	"paratech/desktop/internal/domain/agent/post"
	"paratech/desktop/internal/domain/agent/services/today"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/testenv"
)

// The adapters satisfy the ports of todayfeed.
var (
	_ todayfeed.PostRepository   = (*sqlite.TodayPostRepository)(nil)
	_ todayfeed.TranscriptReader = (*sqlite.TranscriptRepository)(nil)
	_ todayfeed.SummaryReader    = (*sqlite.SummaryRepository)(nil)
	_ todayfeed.RecordingReader  = (*sqlite.RecordingRepository)(nil)
	_ todayfeed.LessonReader     = (*sqlite.LessonRepository)(nil)
	_ todayfeed.AssignmentReader = (*sqlite.AssignmentRepository)(nil)
	_ todayfeed.ProposalList     = (*sqlite.ProposalRepository)(nil)
)

// noAssignments is the Assignments port with nothing listed.
type noAssignments struct{}

func (noAssignments) Deadlines(context.Context) ([]today.Deadline, error) { return nil, nil }
func (noAssignments) Label(context.Context, shared.AssignmentID) (string, string, error) {
	return "", "", shared.ErrNotFound
}

// noSubjects is the SubjectResolver with an empty library.
type noSubjects struct{}

func (noSubjects) ResolveSubjects(context.Context, []string) (map[string]subjectmatch.Match, error) {
	return map[string]subjectmatch.Match{}, nil
}

func (e *ppEnv) today() (*todayfeed.Poster, *todayfeed.Screen) {
	e.t.Helper()
	posts := sqlite.NewTodayPostRepository(e.db)
	poster, err := todayfeed.NewPoster(todayfeed.PosterDeps{
		Posts: posts, Transcripts: sqlite.NewTranscriptRepository(e.db), Summaries: e.summaries, Clock: e.clock,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	screen, err := todayfeed.NewScreen(todayfeed.ScreenDeps{
		Posts: posts, Recordings: sqlite.NewRecordingRepository(e.db), Lessons: sqlite.NewLessonRepository(e.db),
		Homework: e.assignments, Assignments: noAssignments{}, Proposals: sqlite.NewProposalRepository(e.db),
		Subjects: noSubjects{}, Clock: e.clock,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return poster, screen
}

// After the lesson's jobs completed, the post is «Конспект готов · Найдено ДЗ до 16.10» with both deadlines for the
// calendar; each completion reported twice still gives one post.
func TestTodayPostAfterTheLesson(t *testing.T) {
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, ppSummary)
	llm.Answer(values.OperationExtractHomework, ppExtract)
	e := newPPEnv(t, llm)
	e.start()
	summarize := e.waitPost(homeworkflow.SummarizeJobType, job.StatusCompleted)
	extract := e.waitPost(homeworkflow.ExtractHomeworkJobType, job.StatusCompleted)

	poster, screen := e.today()
	ctx := context.Background()
	for range 2 {
		for _, j := range []*job.Job{summarize, extract} {
			if err := poster.JobCompleted(ctx, j.Snapshot()); err != nil {
				t.Fatal(err)
			}
		}
	}
	stored, err := sqlite.NewTodayPostRepository(e.db).ListUpdatedSince(ctx, time.Time{}, 10)
	if err != nil || len(stored) != 1 || stored[0].Version() != 2 || stored[0].Recording() != "r1" {
		t.Fatalf("stored posts = %v, %v; want one post of r1 at version 2", stored, err)
	}

	got, err := screen.Today(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Posts) != 1 {
		t.Fatalf("posts = %+v", got.Posts)
	}
	p := got.Posts[0]
	if p.Headline != "Конспект готов · Найдено ДЗ до 16.10" || p.Homework != 2 || len(p.Calendar) != 2 ||
		p.Lesson.Lesson != "l1" || p.Lesson.Title != "Geography" {
		t.Fatalf("post = %+v", p)
	}
	if len(got.Lessons) != 1 || got.Lessons[0].Post == nil || got.Lessons[0].Post.Recording != "r1" {
		t.Fatalf("lessons = %+v", got.Lessons)
	}
	if len(got.Done) != 1 {
		t.Fatalf("done = %+v", got.Done)
	}
}

// TASK-110: a lesson without speech gets no post-processing, and even a summary event for its transcript makes no
// post: «Сегодня» shows the lesson without a post.
func TestTodayNoPostForALessonWithoutSpeech(t *testing.T) {
	segs := []transcript.Segment{{StartMs: 0, EndMs: 10_000, Text: " [BLANK_AUDIO]"}}
	e := newPPEnvWith(t, testenv.NewFakeLLM(), segs)
	e.start()
	testenv.Eventually(t, "stt job failed", func() bool {
		j, err := e.jobs.Get(context.Background(), "stt-1")
		return err == nil && j.Status() == job.StatusFailed
	}, func() { e.clock.Advance(time.Second) })

	poster, screen := e.today()
	ctx := context.Background()
	if err := poster.Record(ctx, post.EventSummary, ppTranscript); err != nil {
		t.Fatal(err)
	}
	stored, err := sqlite.NewTodayPostRepository(e.db).ListUpdatedSince(ctx, time.Time{}, 10)
	if err != nil || len(stored) != 0 {
		t.Fatalf("stored posts = %v, %v; want none", stored, err)
	}
	got, err := screen.Today(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Posts) != 0 || len(got.Done) != 0 || len(got.Lessons) != 1 || got.Lessons[0].Post != nil {
		t.Fatalf("today = %+v, want the lesson without a post", got)
	}
}
