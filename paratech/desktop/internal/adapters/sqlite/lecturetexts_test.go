package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/shared"
)

func lectureBlock(t *testing.T, heading string, start int64, paragraphs ...string) lecturetext.Block {
	t.Helper()
	b, err := lecturetext.NewBlock(heading, start, paragraphs)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newTestLectureText(t *testing.T, transcript shared.TranscriptID) *lecturetext.LectureText {
	t.Helper()
	l, err := lecturetext.New(lecturetext.Spec{Transcript: transcript, Lesson: "l1"}, asgT0)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// A run is stored part by part and survives a reload; the published text replaces the one before.
func TestLectureTextSaveGetRunAndPublish(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	repo := NewLectureTextRepository(e.db)
	l := newTestLectureText(t, "t1")
	a, b := lectureBlock(t, "Реки", 0, "Сегодня о реках.", "Дельты."), lectureBlock(t, "Озёра", 61_000, "Озёра.")

	if err := l.AddPart("run1", 2, 0, []lecturetext.Block{a}, asgT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, l); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Get(ctx, lecturetext.IDOf("t1"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.State(), l.State()) || got.NextPart("run1", 2) != 1 || got.Version() != 1 {
		t.Fatalf("got %+v, want %+v", got.State(), l.State())
	}

	if err := got.AddPart("run1", 2, 1, []lecturetext.Block{b}, asgT0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := got.Publish("run1", []lecturetext.Block{a, b}, 1, asgT0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("Save after Publish: %v", err)
	}
	again, err := repo.Get(ctx, lecturetext.IDOf("t1"))
	if err != nil {
		t.Fatal(err)
	}
	if again.Version() != 2 || !again.Ready() || len(again.Blocks()) != 2 || !again.Blocks()[1].Equal(b) || again.PromptVersion() != 1 {
		t.Fatalf("after Publish: %+v", again.State())
	}
	if run, _, _ := again.Run(); run != "" {
		t.Fatalf("run after Publish = %q", run)
	}
}

func TestLectureTextSaveConflicts(t *testing.T) {
	ctx := context.Background()
	t.Run("second insert", func(t *testing.T) {
		e := newCourseworkEnv(t)
		repo := NewLectureTextRepository(e.db)
		if err := repo.Save(ctx, newTestLectureText(t, "t1")); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, newTestLectureText(t, "t1")); !errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("second insert = %v, want ErrVersionConflict", err)
		}
	})
	t.Run("stale version", func(t *testing.T) {
		e := newCourseworkEnv(t)
		repo := NewLectureTextRepository(e.db)
		l := newTestLectureText(t, "t1")
		if err := repo.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
		stale, err := repo.Get(ctx, l.ID())
		if err != nil {
			t.Fatal(err)
		}
		if err := l.AddPart("r", 1, 0, nil, asgT0); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
		if err := stale.AddPart("r2", 1, 0, nil, asgT0); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("stale Save = %v, want ErrVersionConflict", err)
		}
	})
	t.Run("unknown transcript", func(t *testing.T) {
		e := newCourseworkEnv(t)
		if err := NewLectureTextRepository(e.db).Save(ctx, newTestLectureText(t, "missing")); !errors.Is(err, shared.ErrConflict) {
			t.Fatalf("Save with an unknown transcript = %v, want ErrConflict", err)
		}
	})
}

func TestLectureTextNotFoundAndCorruptRows(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	repo := NewLectureTextRepository(e.db)
	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
	rows := map[string]string{
		"blocks not an array": `'{"a":1}', 0, '', 0, '[]'`,
		"unknown block field": `'[{"heading":"a","start_ms":0,"paragraphs":["p"],"html":"x"}]', 1, '', 0, '[]'`,
		"invalid block":       `'[{"heading":"","start_ms":0,"paragraphs":["p"]}]', 1, '', 0, '[]'`,
		"parts not arrays":    `'[]', 0, 'r', 1, '[1]'`,
		"parts without a run": `'[]', 0, '', 0, '[[]]'`,
	}
	for name, values := range rows {
		t.Run(name, func(t *testing.T) {
			mustExec(t, e.db, "DELETE FROM lecture_texts")
			mustExec(t, e.db, `INSERT INTO lecture_texts (id, transcript_id, blocks, prompt_version, run, run_parts, parts, version, created_at, updated_at)
				VALUES ('t1', 't1', `+values+`, 1, 1, 1)`)
			if _, err := repo.Get(ctx, "t1"); err == nil || errors.Is(err, shared.ErrNotFound) {
				t.Fatalf("Get of a corrupt row = %v, want an error", err)
			}
		})
	}
}

// A lecture text stays when its transcript is deleted, like the summary.
func TestLectureTextSurvivesTranscriptDeletion(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	repo := NewLectureTextRepository(e.db)
	if err := repo.Save(ctx, newTestLectureText(t, "t1")); err != nil {
		t.Fatal(err)
	}
	mustExec(t, e.db, "DELETE FROM transcripts WHERE id = 't1'")
	got, err := repo.Get(ctx, lecturetext.IDOf("t1"))
	if err != nil {
		t.Fatalf("Get after the transcript was deleted: %v", err)
	}
	if got.TranscriptID() != "" {
		t.Fatalf("transcript reference = %q, want empty", got.TranscriptID())
	}
}
