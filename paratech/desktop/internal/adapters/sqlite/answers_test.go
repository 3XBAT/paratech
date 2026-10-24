package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/shared"
)

var ansT0 = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

func newAnswer(t *testing.T, id shared.AnswerID, subject shared.SubjectID, question string) *answer.Answer {
	t.Helper()
	req, err := answer.NewRequest(question, []answer.Excerpt{
		{Text: "Производная функции — это предел отношения приращения функции к приращению аргумента.",
			Source: answer.Source{Recording: "rec1", Section: answer.SectionLectureText, Index: 2, StartMs: 754_000,
				Title: "Матанализ", StartedAt: ansT0.Add(-time.Hour)}},
		{Text: "Ключевые понятия: производная, дифференциал.",
			Source: answer.Source{Recording: "rec2", Section: answer.SectionSummary}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := req.Parse(`{"claims":[` +
		`{"text":"Производная — предел отношения.","cites":[{"chunk_id":"f1","quote":"предел отношения приращения функции"}]},` +
		`{"text":"Выдумка.","cites":[{"chunk_id":"f2","quote":"чего нет в конспекте вовсе"}]}` +
		`],"missing":["Геометрический смысл"]}`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := answer.New(id, subject, req, rep, ansT0)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAnswerInsertGet(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAnswerRepository(db)
	a := newAnswer(t, "an1", "s1", "Что такое производная?")

	if _, err := repo.Get(ctx, "an1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get of none = %v", err)
	}
	if _, err := repo.FindByKey(ctx, a.Key()); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("FindByKey of none = %v", err)
	}
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	for name, get := range map[string]func() (*answer.Answer, error){
		"Get":       func() (*answer.Answer, error) { return repo.Get(ctx, "an1") },
		"FindByKey": func() (*answer.Answer, error) { return repo.FindByKey(ctx, a.Key()) },
	} {
		got, err := get()
		if err != nil || !reflect.DeepEqual(got.State(), a.State()) {
			t.Fatalf("%s = %+v, %v\nwant %+v", name, got.State(), err, a.State())
		}
	}

	// A question about the whole base keeps no subject.
	whole := newAnswer(t, "an2", "", "Что такое дифференциал?")
	if err := repo.Insert(ctx, whole); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Get(ctx, "an2"); err != nil || got.Subject() != "" || len(got.Sources()) != 1 {
		t.Fatalf("Get(whole base) = %+v, %v", got, err)
	}

	// One answer per request key, one per ID.
	if err := repo.Insert(ctx, newAnswer(t, "an3", "s1", "Что такое производная?")); !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("same key = %v", err)
	}
	if err := repo.Insert(ctx, newAnswer(t, "an1", "s1", "Что такое ряд производных?")); !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("same id = %v", err)
	}
}

// A stored answer that breaks the schema or the aggregate's invariants is refused on reading, never shown.
func TestAnswerCorruptRow(t *testing.T) {
	tests := map[string]string{
		"unknown field":  `UPDATE answers SET claims = '[{"text":"x","cites":[],"send":true}]'`,
		"bad source":     `UPDATE answers SET sources = '[{"recording":"rec1","section":"title","index":0,"start_ms":0,"title":"","started_at":0}]'`,
		"citation index": `UPDATE answers SET claims = '[{"text":"x","cites":[{"quote":"q","source":7}]}]'`,
		"missing object": `UPDATE answers SET missing = '{"a":1}'`,
		"blank missing":  `UPDATE answers SET missing = '[" "]'`,
	}
	for name, stmt := range tests {
		t.Run(name, func(t *testing.T) {
			db, ctx := openTest(t, nil), context.Background()
			repo := NewAnswerRepository(db)
			if err := repo.Insert(ctx, newAnswer(t, "an1", "s1", "Что такое производная?")); err != nil {
				t.Fatal(err)
			}
			if _, err := db.sql.ExecContext(ctx, stmt); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Get(ctx, "an1"); err == nil {
				t.Fatal("a corrupt answer was read")
			}
		})
	}
}
