package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/shared"
)

func newTestSummary(t *testing.T, transcript shared.TranscriptID, points ...string) *summary.Summary {
	t.Helper()
	c, err := summary.NewContent("The lesson was about rivers.\nAnd lakes.", points)
	if err != nil {
		t.Fatal(err)
	}
	s, err := summary.New(summary.Spec{Transcript: transcript, Lesson: "l1", PromptVersion: 1, Content: c}, asgT0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSummarySaveGetReplace(t *testing.T) {
	for _, points := range [][]string{{"Rivers flow", "Lakes do not"}, nil} {
		t.Run("", func(t *testing.T) {
			e, ctx := newCourseworkEnv(t), context.Background()
			s := newTestSummary(t, "t1", points...)
			if err := e.summaries.Save(ctx, s); err != nil {
				t.Fatalf("Save: %v", err)
			}
			got, err := e.summaries.Get(ctx, summary.IDOf("t1"))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.State(), s.State()) {
				t.Fatalf("got %+v, want %+v", got.State(), s.State())
			}

			c, err := summary.NewContent("Shorter.", []string{"One"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := got.Replace(c, 2, asgT0.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if err := e.summaries.Save(ctx, got); err != nil {
				t.Fatalf("Save after Replace: %v", err)
			}
			again, err := e.summaries.Get(ctx, summary.IDOf("t1"))
			if err != nil {
				t.Fatal(err)
			}
			if again.Version() != 2 || !again.Content().Equal(c) || again.PromptVersion() != 2 {
				t.Fatalf("after Replace: %+v", again.State())
			}
		})
	}
}

func TestSummarySaveConflicts(t *testing.T) {
	ctx := context.Background()
	t.Run("second insert", func(t *testing.T) {
		e := newCourseworkEnv(t)
		if err := e.summaries.Save(ctx, newTestSummary(t, "t1")); err != nil {
			t.Fatal(err)
		}
		if err := e.summaries.Save(ctx, newTestSummary(t, "t1")); !errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("second insert = %v, want ErrVersionConflict", err)
		}
	})
	t.Run("stale version", func(t *testing.T) {
		e := newCourseworkEnv(t)
		s := newTestSummary(t, "t1")
		if err := e.summaries.Save(ctx, s); err != nil {
			t.Fatal(err)
		}
		stale, err := e.summaries.Get(ctx, s.ID())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Replace(s.Content(), 2, asgT0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := e.summaries.Save(ctx, s); err != nil {
			t.Fatal(err)
		}
		if _, err := stale.Replace(s.Content(), 3, asgT0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := e.summaries.Save(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("stale Save = %v, want ErrVersionConflict", err)
		}
	})
	t.Run("unknown transcript", func(t *testing.T) {
		e := newCourseworkEnv(t)
		if err := e.summaries.Save(ctx, newTestSummary(t, "missing")); !errors.Is(err, shared.ErrConflict) {
			t.Fatalf("Save with an unknown transcript = %v, want ErrConflict", err)
		}
	})
}

func TestSummaryNotFoundAndCorruptRows(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	if _, err := e.summaries.Get(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
	mustExec(t, e.db, `INSERT INTO summaries (id, transcript_id, body, key_points, prompt_version, version, created_at, updated_at)
		VALUES ('t1', 't1', 'x', '{"a":1}', 1, 1, 1, 1)`)
	if _, err := e.summaries.Get(ctx, "t1"); err == nil {
		t.Fatal("Get of a row whose key points are not an array of strings succeeded")
	}
}

// A summary stays when its transcript is deleted; it is lesson content, not a copy of the transcript.
func TestSummarySurvivesTranscriptDeletion(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	if err := e.summaries.Save(ctx, newTestSummary(t, "t1")); err != nil {
		t.Fatal(err)
	}
	mustExec(t, e.db, "DELETE FROM transcripts WHERE id = 't1'")
	got, err := e.summaries.Get(ctx, summary.IDOf("t1"))
	if err != nil {
		t.Fatalf("Get after the transcript was deleted: %v", err)
	}
	if got.TranscriptID() != "" {
		t.Fatalf("transcript reference = %q, want empty", got.TranscriptID())
	}
}
