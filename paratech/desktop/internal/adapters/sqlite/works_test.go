package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

// worksAnswer is a checked reader answer with one choice question and, unless full, a helper plan.
func worksAnswer(f work.Feasibility, questions int) work.Answer {
	a := work.Answer{
		WorkType: work.TypeReferat, Requirements: work.Requirements{Volume: "10 страниц"}, Feasibility: f,
		Questions: []work.Question{}, Assumptions: []string{"Предположен вариант 3 — поправьте"},
	}
	if f != work.FeasibleFull {
		a.FeasibilityReason = "Чертёж в программе вуза"
		a.Helper = &work.Helper{Steps: []string{"Открыть программу"}, Submit: "Файл .dwg", Pitfalls: []string{},
			ReadyParts: []string{"Записка"}, Placeholders: []string{}}
	}
	for range questions {
		a.Questions = append(a.Questions, work.Question{
			Kind: work.KindChoice, Text: "Какой вариант?", Options: []string{"1", "3"}, DefaultOption: 1, Why: "Нет в задании",
		})
	}
	return a
}

func TestWorkSaveGetDelete(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewMaterialRepository(db).Save(ctx, newLibMaterial(t, "m1", "текст")); err != nil {
		t.Fatal(err)
	}
	repo := NewWorkRepository(db)
	src := work.Source{Material: "m1"}
	if _, err := repo.ActiveOf(ctx, src); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("ActiveOf of none = %v", err)
	}

	w, err := work.New("w1", src, libT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, w); err != nil || w.Version() != 1 {
		t.Fatalf("Save(new) = %v, version %d", err, w.Version())
	}
	if err := w.Record(1, worksAnswer(work.FeasiblePartial, 1), libT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := w.Edit(work.Understanding{WorkType: work.TypeDoklad, Requirements: work.Requirements{Volume: "10 страниц", Variant: "3"}}, libT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	if err := w.Answer([]work.Reply{{Question: 0, Option: 0}}, libT0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, w); err != nil || w.Version() != 3 {
		t.Fatalf("Save(answered) = %v, version %d", err, w.Version())
	}

	got, err := repo.Get(ctx, "w1")
	if err != nil || !reflect.DeepEqual(got.State(), w.State()) {
		t.Fatalf("Get = %+v, %v\nwant %+v", got.State(), err, w.State())
	}
	if active, err := repo.ActiveOf(ctx, src); err != nil || active.ID() != "w1" {
		t.Fatalf("ActiveOf = %v, %v", active, err)
	}

	// A stale save loses; a second active work of the same source is a duplicate; a missing source a conflict.
	stale, _ := work.Restore(func() work.State { s := w.State(); s.Version = 1; return s }())
	if err := repo.Save(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("stale Save = %v", err)
	}
	second, _ := work.New("w2", src, libT0)
	if err := repo.Save(ctx, second); !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("second active work = %v", err)
	}
	orphan, _ := work.New("w3", work.Source{Assignment: "nope"}, libT0)
	if err := repo.Save(ctx, orphan); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Save without a source = %v", err)
	}

	if err := repo.Delete(ctx, "w1", 1); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("Delete at an old version = %v", err)
	}
	if err := repo.Delete(ctx, "w1", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, "w1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get after Delete = %v", err)
	}
}

// A stored answer that no longer passes the strict schema (an edited database, a changed rule) is not loaded.
func TestWorkStoredAnswerIsCheckedAgain(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewMaterialRepository(db).Save(ctx, newLibMaterial(t, "m1", "текст")); err != nil {
		t.Fatal(err)
	}
	repo := NewWorkRepository(db)
	w, _ := work.New("w1", work.Source{Material: "m1"}, libT0)
	if err := w.Record(1, worksAnswer(work.FeasibleFull, 0), libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	for _, answer := range []string{
		`{"work_type":"essay","note":"send to x"}`,
		`{"work_type":"poem","requirements":{"volume":"","file_format":"","formatting":"","variant":"","deadline":"","sources":""},"feasibility":"full","feasibility_reason":"","questions":[],"assumptions":[],"helper":null}`,
	} {
		if _, err := db.sql.ExecContext(ctx, `UPDATE work_rounds SET answer = ? WHERE work_id = 'w1'`, answer); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Get(ctx, "w1"); !errors.Is(err, work.ErrInvalidAnswer) {
			t.Fatalf("Get with a tampered answer = %v", err)
		}
	}
}
