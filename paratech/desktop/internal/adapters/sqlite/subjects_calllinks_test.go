package sqlite

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/subject"
)

// Fake links per kind: reserved domains, made-up meetings and passwords.
var subjectKindLinks = map[subject.CallKind]string{
	subject.CallGeneral: subjectCallLink,
	subject.CallLecture: "https://lecture.example.org/j/222?pwd=FakeLec2",
	subject.CallSeminar: "https://seminar.example.org/j/333?pwd=FakeSem3",
	subject.CallLab:     "https://lab.example.org/j/444?pwd=FakeLab4",
}

// CallLinks() round-trips through SQLite for every kind on insert and on update; each kind is its own column and a
// clear of one keeps the others.
func TestSubjectCallLinksRoundTrip(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewSubjectRepository(db)

	s := newLibSubject(t, "s1", "Физика")
	if _, err := s.SetCallLinkOf(subject.CallLecture, subjectKindLinks[subject.CallLecture], libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Get(ctx, "s1")
	if err != nil || got.CallLinks() != s.CallLinks() || !reflect.DeepEqual(got.State(), s.State()) {
		t.Fatalf("Get after insert = %v, %v", got.CallLinks(), err)
	}

	t1 := libT0.Add(time.Hour)
	for k, link := range subjectKindLinks {
		if _, err := got.SetCallLinkOf(k, link, t1); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("Save of a change: %v", err)
	}
	list, err := repo.List(ctx)
	if err != nil || len(list) != 1 || list[0].CallLinks() != got.CallLinks() || list[0].Version() != 2 {
		t.Fatalf("List = %v, %v", list, err)
	}
	for k, link := range subjectKindLinks {
		if list[0].CallLinks().Of(k).String() != link {
			t.Errorf("%s read back as %v", k, list[0].CallLinks().Of(k))
		}
	}
	if n := scanInt(t, db.sql, `SELECT count(*) FROM subjects WHERE call_link = ? AND call_link_lecture = ?
		AND call_link_seminar = ? AND call_link_lab = ?`, subjectKindLinks[subject.CallGeneral],
		subjectKindLinks[subject.CallLecture], subjectKindLinks[subject.CallSeminar], subjectKindLinks[subject.CallLab]); n != 1 {
		t.Fatalf("the columns do not hold the links of their kinds: %d rows", n)
	}

	if _, err := list[0].ClearCallLinkOf(subject.CallSeminar, t1.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, list[0]); err != nil {
		t.Fatalf("Save of the clear: %v", err)
	}
	cleared, err := repo.Get(ctx, "s1")
	if err != nil || !cleared.CallLinks().Seminar.IsZero() || cleared.CallLinks().Lab.String() != subjectKindLinks[subject.CallLab] ||
		cleared.CallLink().String() != subjectCallLink || cleared.Version() != 3 {
		t.Fatalf("after the clear: %v, version %d, %v", cleared.CallLinks(), cleared.Version(), err)
	}
}

// A stored value of a kind that no longer parses is read as no link of that kind, and the subject is still read; each
// column refuses more than 2048 bytes.
func TestSubjectCallLinksStoredBadValue(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewSubjectRepository(db)
	if err := repo.Save(ctx, newLibSubject(t, "s1", "Физика")); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"call_link_lecture", "call_link_seminar", "call_link_lab"} {
		mustExec(t, db, "UPDATE subjects SET "+column+" = 'javascript:alert(1)' WHERE id = 's1'")
		if got, err := repo.Get(ctx, "s1"); err != nil || !got.CallLinks().IsZero() {
			t.Errorf("%s bad: Get = %v, %v", column, got, err)
		}
		long := "https://a.example.org/" + strings.Repeat("x", 2048)
		if _, err := db.sql.ExecContext(ctx, "UPDATE subjects SET "+column+" = ? WHERE id = 's1'", long); err == nil {
			t.Errorf("%s: a call link over 2048 bytes was stored", column)
		}
	}
}

// Migration 0052 on real SQLite: existing subjects keep their general link and get no link per kind (nothing is
// copied); down drops the three columns and keeps the subjects with their general link.
func TestMigrationSubjectCallLinksByKind(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, nil)
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, subjectCallLinksByKindVersion-1); err != nil {
		t.Fatalf("down to %d: %v", subjectCallLinksByKindVersion-1, err)
	}
	mustExec(t, db, `INSERT INTO subjects (id, name, name_key, semester, call_link, version, created_at, updated_at)
		VALUES ('s1', 'Физика', 'физика', '2026-autumn', ?, 1, 1, 1)`, subjectCallLink)
	if _, err := p.UpTo(ctx, subjectCallLinksByKindVersion); err != nil {
		t.Fatalf("up to %d: %v", subjectCallLinksByKindVersion, err)
	}
	if n := scanInt(t, db.sql, `SELECT count(*) FROM subjects WHERE id = 's1' AND call_link = ? AND call_link_lecture = ''
		AND call_link_seminar = '' AND call_link_lab = ''`, subjectCallLink); n != 1 {
		t.Fatal("the existing subject did not keep its general link with no link per kind")
	}

	mustExec(t, db, "UPDATE subjects SET call_link_lecture = ? WHERE id = 's1'", subjectKindLinks[subject.CallLecture])
	if _, err := p.DownTo(ctx, subjectCallLinksByKindVersion-1); err != nil {
		t.Fatalf("down again: %v", err)
	}
	if n := scanInt(t, db.sql, `SELECT count(*) FROM pragma_table_info('subjects')
		WHERE name IN ('call_link_lecture', 'call_link_seminar', 'call_link_lab')`); n != 0 {
		t.Fatalf("%d call link columns per kind exist after down", n)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subjects WHERE id = 's1' AND call_link = ?", subjectCallLink); n != 1 {
		t.Fatal("down lost the subject or its general link")
	}
}
