package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// A fake call link: a reserved domain, a made-up meeting and password.
const subjectCallLink = "https://meet.example.org/j/1234567890?pwd=FakePwd42"

// The call link of a subject is stored on insert and on update, read back by Get and List, and cleared.
func TestSubjectCallLinkRoundTrip(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewSubjectRepository(db)

	s := newLibSubject(t, "s1", "Физика")
	if _, err := s.SetCallLink(subjectCallLink, libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Get(ctx, "s1")
	if err != nil || !reflect.DeepEqual(got.State(), s.State()) || got.CallLink().String() != subjectCallLink {
		t.Fatalf("Get = %v, %v", got, err)
	}

	t1 := libT0.Add(time.Hour)
	const other = "https://bbb.example.edu/b/room"
	if _, err := got.SetCallLink(other, t1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("Save of a change: %v", err)
	}
	list, err := repo.List(ctx)
	if err != nil || len(list) != 1 || list[0].CallLink().String() != other || list[0].Version() != 2 {
		t.Fatalf("List = %v, %v", list, err)
	}

	if changed, err := list[0].ClearCallLink(t1.Add(time.Hour)); err != nil || !changed {
		t.Fatalf("ClearCallLink = %v, %v", changed, err)
	}
	if err := repo.Save(ctx, list[0]); err != nil {
		t.Fatalf("Save of the clear: %v", err)
	}
	cleared, err := repo.Get(ctx, "s1")
	if err != nil || !cleared.CallLink().IsZero() || cleared.Version() != 3 {
		t.Fatalf("after clear: %v, version %d, %v", cleared.CallLink(), cleared.Version(), err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subjects WHERE call_link = ''"); n != 1 {
		t.Fatalf("stored call_link not cleared: %d empty", n)
	}
}

// A save at a stale version keeps the stored link: it is written under the version check.
func TestSubjectCallLinkSaveUnderVersion(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewSubjectRepository(db)
	if err := repo.Save(ctx, newLibSubject(t, "s1", "Физика")); err != nil {
		t.Fatal(err)
	}
	first, err := repo.Get(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Get(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.SetCallLink(subjectCallLink, libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := second.SetCallLink("https://bbb.example.edu/b/room", libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, second); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("stale Save = %v, want ErrVersionConflict", err)
	}
	stored, err := repo.Get(ctx, "s1")
	if err != nil || stored.CallLink().String() != subjectCallLink || stored.Version() != 2 {
		t.Fatalf("stored %v at version %d, %v", stored.CallLink(), stored.Version(), err)
	}
}

// A stored value that no longer parses is read as no link, and the subject is still read (ADR-0032); the column
// refuses more than 2048 bytes.
func TestSubjectCallLinkStoredBadValue(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewSubjectRepository(db)
	if err := repo.Save(ctx, newLibSubject(t, "s1", "Физика")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"javascript:alert(1)", "https://localhost/", "HTTPS://meet.example.org/"} {
		mustExec(t, db, "UPDATE subjects SET call_link = ? WHERE id = 's1'", bad)
		got, err := repo.Get(ctx, "s1")
		if err != nil || !got.CallLink().IsZero() {
			t.Errorf("stored %q: Get = %v, %v", bad, got, err)
		}
		if list, err := repo.List(ctx); err != nil || len(list) != 1 || !list[0].CallLink().IsZero() {
			t.Errorf("stored %q: List = %v, %v", bad, list, err)
		}
	}
	long := "https://a.example.org/" + strings.Repeat("x", 2048)
	if _, err := db.sql.ExecContext(ctx, "UPDATE subjects SET call_link = ? WHERE id = 's1'", long); err == nil {
		t.Fatal("a call link over 2048 bytes was stored")
	}
}

// Migration 0050 on real SQLite: existing subjects get no link; down drops the column and keeps the subjects.
func TestMigrationSubjectCallLink(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, nil)
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, subjectCallLinkVersion-1); err != nil {
		t.Fatalf("down to %d: %v", subjectCallLinkVersion-1, err)
	}
	mustExec(t, db, `INSERT INTO subjects (id, name, name_key, semester, version, created_at, updated_at)
		VALUES ('s1', 'Физика', 'физика', '2026-autumn', 1, 1, 1)`)
	if _, err := p.UpTo(ctx, subjectCallLinkVersion); err != nil {
		t.Fatalf("up to %d: %v", subjectCallLinkVersion, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subjects WHERE id = 's1' AND call_link = ''"); n != 1 {
		t.Fatal("the existing subject did not get an empty link")
	}
	mustExec(t, db, "UPDATE subjects SET call_link = ? WHERE id = 's1'", subjectCallLink)
	if _, err := p.DownTo(ctx, subjectCallLinkVersion-1); err != nil {
		t.Fatalf("down again: %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM pragma_table_info('subjects') WHERE name = 'call_link'"); n != 0 {
		t.Fatal("call_link exists after down")
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subjects WHERE id = 's1'"); n != 1 {
		t.Fatal("down lost the subject")
	}
}
