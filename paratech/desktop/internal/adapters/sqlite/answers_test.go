package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
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
	}, "ag:th1:1")
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

// Keys of both versions are stored and found: v2 (with the reuse scope) and v1, made before it (migration 0030).
func TestAnswerKeyVersions(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAnswerRepository(db)
	v2 := newAnswer(t, "an1", "s1", "Что такое производная?")
	st := newAnswer(t, "an2", "s1", "Что такое дифференциал?").State()
	st.Key = "ans:v1:" + strings.Repeat("0f", 32)
	v1, err := answer.Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v2.Key(), "ans:v2:") {
		t.Fatalf("new key %q", v2.Key())
	}
	for _, a := range []*answer.Answer{v2, v1} {
		if err := repo.Insert(ctx, a); err != nil {
			t.Fatalf("Insert %s: %v", a.Key(), err)
		}
		if got, err := repo.FindByKey(ctx, a.Key()); err != nil || got.ID() != a.ID() {
			t.Fatalf("FindByKey(%s) = %v, %v", a.Key(), got, err)
		}
	}

	// The schema still refuses a key of another shape.
	if _, err := db.sql.ExecContext(ctx, `UPDATE answers SET request_key = 'ans:v3:' || substr(request_key, 8) WHERE id = 'an1'`); err == nil {
		t.Fatal("a v3 key was stored")
	}
}

// Version 30 rebuilds answers keeping the rows of version 29 (keys v1) and accepting v2; down to 29 keeps v1 only.
func TestMigrationToVersion30KeepsAnswers(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 29); err != nil {
		t.Fatalf("up to 29: %v", err)
	}
	st := newAnswer(t, "an1", "s1", "Что такое производная?").State()
	st.Key = "ans:v1:" + strings.Repeat("0f", 32)
	v1, err := answer.Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewAnswerRepository(db)
	if err := repo.Insert(ctx, v1); err != nil {
		t.Fatalf("Insert at 29: %v", err)
	}
	if err := repo.Insert(ctx, newAnswer(t, "an2", "s1", "Что такое дифференциал?")); err == nil {
		t.Fatal("a v2 key was stored at version 29")
	}

	if _, err := p.UpTo(ctx, 30); err != nil {
		t.Fatalf("up to 30: %v", err)
	}
	if got, err := repo.FindByKey(ctx, v1.Key()); err != nil || !reflect.DeepEqual(got.State(), v1.State()) {
		t.Fatalf("v1 answer after the migration = %v, %v", got, err)
	}
	if err := repo.Insert(ctx, newAnswer(t, "an2", "s1", "Что такое дифференциал?")); err != nil {
		t.Fatalf("Insert v2 at 30: %v", err)
	}
	if err := repo.Insert(ctx, newAnswer(t, "an3", "s1", "Что такое дифференциал?")); !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("same key after the rebuild = %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM sqlite_master WHERE name LIKE 'answers\\_v%' ESCAPE '\\'"); n != 0 {
		t.Fatalf("%d leftover objects of the rebuild", n)
	}

	if _, err := p.DownTo(ctx, 29); err != nil {
		t.Fatalf("down to 29: %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM answers"); n != 1 {
		t.Fatalf("answers after down = %d, want the v1 one", n)
	}
}

// A source of the student's note (TASK-153) is stored with its note, file and place and read back as it was.
func TestAnswerOwnNoteSource(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAnswerRepository(db)
	note := answer.Source{Section: answer.SectionOwnNote, Index: 1, Title: "Ряды", StartedAt: ansT0.Add(-time.Hour),
		Note: "n1", Material: "m1", Locator: answer.Locator{Kind: answer.LocatorSection, N: 2}, Heading: "Ряд Тейлора"}
	req, err := answer.NewRequest("Что такое ряд Тейлора?", []answer.Excerpt{
		{Text: "Ключевые понятия: ряд.", Source: answer.Source{Recording: "rec1", Section: answer.SectionSummary}},
		{Text: "Ряд Тейлора\nРяд Тейлора приближает функцию многочленом.", Source: note},
	}, "ag:th1:1")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := req.Parse(`{"claims":[{"text":"Ряд приближает функцию.","cites":[{"chunk_id":"f2","quote":"приближает функцию многочленом"}]}],"missing":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := answer.New("an1", "s1", req, rep, ansT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "an1")
	if err != nil || !reflect.DeepEqual(got.State(), a.State()) || got.Sources()[0].Recording != "" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
}

// An answer stored before notes (TASK-118: sources without the note fields) reads unchanged, and a source of a
// recording is still stored in that shape.
func TestAnswerStoredBeforeNotes(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAnswerRepository(db)
	a := newAnswer(t, "an1", "s1", "Что такое производная?")
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	old := `[{"recording":"rec1","section":"lectureText","index":2,"start_ms":754000,"title":"Матанализ","started_at":` +
		strconv.FormatInt(ansT0.Add(-time.Hour).UnixMilli(), 10) + `}]`
	if got := scanString(t, db, "SELECT sources FROM answers WHERE id = 'an1'"); got != old {
		t.Fatalf("stored sources = %s\nwant %s", got, old)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE answers SET sources = ? WHERE id = 'an1'`, old); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "an1")
	if err != nil || !reflect.DeepEqual(got.State(), a.State()) {
		t.Fatalf("old answer = %+v, %v\nwant %+v", got, err, a.State())
	}
}

// A stored answer that breaks the schema or the aggregate's invariants is refused on reading, never shown.
func TestAnswerCorruptRow(t *testing.T) {
	tests := map[string]string{
		"unknown field":         `UPDATE answers SET claims = '[{"text":"x","cites":[],"send":true}]'`,
		"bad source":            `UPDATE answers SET sources = '[{"recording":"rec1","section":"title","index":0,"start_ms":0,"title":"","started_at":0}]'`,
		"note with a recording": `UPDATE answers SET sources = '[{"recording":"rec1","section":"ownNote","index":0,"start_ms":0,"title":"","started_at":0,"note":"n1","material":"m1","locator_kind":"page","locator_n":1}]'`,
		"note without locator":  `UPDATE answers SET sources = '[{"recording":"","section":"ownNote","index":0,"start_ms":0,"title":"","started_at":0,"note":"n1","material":"m1"}]'`,
		"recording with a note": `UPDATE answers SET sources = '[{"recording":"rec1","section":"summary","index":0,"start_ms":0,"title":"","started_at":0,"note":"n1"}]'`,
		"citation index":        `UPDATE answers SET claims = '[{"text":"x","cites":[{"quote":"q","source":7}]}]'`,
		"missing object":        `UPDATE answers SET missing = '{"a":1}'`,
		"blank missing":         `UPDATE answers SET missing = '[" "]'`,
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
