package tests

// Scenario test of the knowledge base (TASK-091, ADR-0019) on the real stack: the post-processed recording r1 of ppEnv
// is found by the search until the recording deletion (TASK-095) purges it; no step of its own removes it from the
// base.

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/textfile"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/recordingdelete"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
)

var (
	_ knowledgebase.LessonStore         = (*sqlite.KnowledgeBase)(nil)
	_ knowledgebase.RecordingRepository = (*sqlite.RecordingRepository)(nil)
	_ knowledgebase.LessonReader        = (*sqlite.LessonRepository)(nil)
	_ knowledgebase.TranscriptReader    = (*sqlite.TranscriptRepository)(nil)
	_ knowledgebase.ArtifactReader      = (*filestore.Store)(nil)
	_ knowledgebase.SummaryReader       = (*sqlite.SummaryRepository)(nil)
	_ knowledgebase.LectureTextReader   = (*sqlite.LectureTextRepository)(nil)
	_ knowledgebase.TextWriter          = textfile.Writer{}
)

func TestKnowledgeBaseLosesAPurgedRecording(t *testing.T) {
	e := newDeleteEnv(t, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	base, err := knowledgebase.New(knowledgebase.Deps{
		Lessons: sqlite.NewKnowledgeBase(e.db), Recordings: e.recordings, Schedule: sqlite.NewLessonRepository(e.db),
		Transcripts: sqlite.NewTranscriptRepository(e.db), Artifacts: e.store, Summaries: e.summaries,
		LectureTexts: sqlite.NewLectureTextRepository(e.db), Writer: textfile.New(), Clock: e.clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	res, err := base.Search(ctx, "RIVERS")
	if err != nil {
		t.Fatal(err)
	}
	sections := map[kb.Section]bool{}
	for _, h := range res.Hits {
		if h.Recording != "r1" {
			t.Fatalf("hit of %s", h.Recording)
		}
		sections[h.Section] = true
	}
	if !sections[kb.SectionSummary] || !sections[kb.SectionTranscript] {
		t.Fatalf("before the deletion: hits %+v, want the summary and the transcript of r1", res.Hits)
	}
	if subjects, err := base.Subjects(ctx); err != nil || len(subjects) != 1 || subjects[0].Lessons != 1 {
		t.Fatalf("Subjects = %+v, %v", subjects, err)
	}
	if _, err := base.SetLessonInfo(ctx, "r1", "География", "lecture"); err != nil {
		t.Fatalf("SetLessonInfo: %v", err)
	}

	s := e.service(10 * time.Second)
	if res, err := s.Delete(ctx, e.request(s)); err != nil || res.Outcome != recordingdelete.OutcomeDeleted {
		t.Fatalf("Delete = %+v, %v", res, err)
	}
	if res, err := base.Search(ctx, "rivers"); err != nil || len(res.Hits) != 0 {
		t.Fatalf("after the deletion: %+v, %v; want nothing", res.Hits, err)
	}
	if subjects, err := base.Subjects(ctx); err != nil || len(subjects) != 0 {
		t.Fatalf("Subjects after the deletion = %+v, %v", subjects, err)
	}
}
