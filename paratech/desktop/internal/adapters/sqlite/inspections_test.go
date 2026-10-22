package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/library/inspection"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

func TestInspectionSaveGet(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewMaterialRepository(db).Save(ctx, newLibMaterial(t, "m1", "текст")); err != nil {
		t.Fatal(err)
	}
	repo := NewInspectionRepository(db)
	if _, err := repo.Get(ctx, "m1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get of none = %v", err)
	}

	d := material.DigestOf([]byte("text"))
	i, err := inspection.New("m1", d, 1, nil, libT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, i); err != nil || i.Version() != 1 {
		t.Fatalf("Save(refused) = %v, version %d", err, i.Version())
	}
	got, err := repo.Get(ctx, "m1")
	if err != nil || !reflect.DeepEqual(got.State(), i.State()) {
		t.Fatalf("Get = %+v, %v", got.State(), err)
	}

	ans := inspection.Answer{
		DocKind: inspection.DocLecture, Subject: "s1", LessonDate: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
		HasHomework: true, MentionsEmail: true,
	}
	if err := got.Record(d, 2, &ans, libT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil || got.Version() != 2 {
		t.Fatalf("Save(done) = %v, version %d", err, got.Version())
	}
	again, err := repo.Get(ctx, "m1")
	if err != nil || !reflect.DeepEqual(again.State(), got.State()) {
		t.Fatalf("Get after the update = %+v, %v\nwant %+v", again.State(), err, got.State())
	}

	// A stale save and a second insert lose; an inspection of a material that does not exist is a conflict.
	if err := repo.Save(ctx, i); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("stale Save = %v", err)
	}
	other, _ := inspection.New("m1", d, 1, nil, libT0)
	if err := repo.Save(ctx, other); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("second insert = %v", err)
	}
	orphan, _ := inspection.New("nope", d, 1, nil, libT0)
	if err := repo.Save(ctx, orphan); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Save without a material = %v", err)
	}
}

// The summary and the assignments of a material (TASK-122) are stored with it and found again by it.
func TestMaterialSummaryAndAssignments(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewMaterialRepository(db).Save(ctx, newLibMaterial(t, "m1", "текст")); err != nil {
		t.Fatal(err)
	}

	c, err := summary.NewContent("Конспект слайдов.", []string{"Пункт"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := summary.New(summary.Spec{Material: "m1", PromptVersion: 1, Content: c}, libT0)
	if err != nil {
		t.Fatal(err)
	}
	sums := NewSummaryRepository(db)
	if err := sums.Save(ctx, s); err != nil {
		t.Fatal(err)
	}
	got, err := sums.Get(ctx, summary.IDOfMaterial("m1"))
	if err != nil || !reflect.DeepEqual(got.State(), s.State()) {
		t.Fatalf("summary = %+v, %v", got, err)
	}

	repo := NewAssignmentRepository(db)
	for i, title := range []string{"Задача 1", "Задача 2"} {
		d, err := assignment.NewDetails(title, "", time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		a, err := assignment.NewExtracted(assignment.Spec{
			ID: shared.AssignmentID("a" + title[len(title)-1:]), Origin: assignment.Origin{Material: "m1", Index: i},
			PromptVersion: 1, Details: d,
		}, libT0)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	a, err := repo.GetByMaterialSource(ctx, "m1", 1)
	if err != nil || a.Details().Title() != "Задача 2" || a.MaterialID() != "m1" {
		t.Fatalf("GetByMaterialSource = %+v, %v", a, err)
	}
	if _, err := repo.GetByMaterialSource(ctx, "m1", 2); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("GetByMaterialSource(missing) = %v", err)
	}
	list, err := repo.ListByMaterial(ctx, "m1")
	if err != nil || len(list) != 2 || list[0].SourceIndex() != 0 {
		t.Fatalf("ListByMaterial = %v, %v", list, err)
	}
	// The transcript queries do not see them.
	if byT, err := repo.ListByTranscript(ctx, "m1"); err != nil || len(byT) != 0 {
		t.Fatalf("ListByTranscript = %v, %v", byT, err)
	}

	// A second assignment of the same item is refused by the unique index.
	d, _ := assignment.NewDetails("Дубль", "", time.Time{})
	dup, _ := assignment.NewExtracted(assignment.Spec{
		ID: "a9", Origin: assignment.Origin{Material: "m1", Index: 0}, PromptVersion: 1, Details: d,
	}, libT0)
	if err := repo.Save(ctx, dup); !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("duplicate item = %v", err)
	}
}
