package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"
)

const lesMeetLink = "https://meet.google.com/abc-defg-hij"

// The call link of a lesson is written on insert and on update, read back, changed and dropped.
func TestLessonCallLinkRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewLessonRepository(openTest(t, nil))

	l := newTestLesson(t, "l1", "ics:uid:1", 0)
	c := l.Content()
	c.CallLink = lesMeetLink
	if _, err := l.ChangeContent(c, lesT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "l1")
	if err != nil || got.Content().CallLink != lesMeetLink || !got.Content().Equal(l.Content()) {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	for i, link := range []string{"https://us02web.zoom.us/j/81234567890?pwd=AbCdEf", ""} {
		c := got.Content()
		c.CallLink = link
		if _, err := got.ChangeContent(c, lesT0.Add(time.Duration(i+2)*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, got); err != nil {
			t.Fatalf("Save of link %q: %v", link, err)
		}
		live, err := repo.ListLive(ctx)
		if err != nil || len(live) != 1 || live[0].Content().CallLink != link {
			t.Fatalf("ListLive after link %q: %v, %v", link, live, err)
		}
		got = live[0]
	}
}

// A lesson stored before the call link existed reads back without a link.
func TestLessonCallLinkLegacyRow(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, lessonCallLinkVersion-1); err != nil {
		t.Fatalf("up to %d: %v", lessonCallLinkVersion-1, err)
	}
	mustExec(t, db, `INSERT INTO schedule_entries (id, title, starts_at, ends_at, timezone, content_revision, version, created_at, updated_at)
		VALUES ('old', 'Матан', 1000, 2000, 'Europe/Moscow', 1, 1, 1, 1)`)
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got, err := NewLessonRepository(db).Get(ctx, "old")
	if err != nil || got.Content().CallLink != "" || got.Content().Title != "Матан" {
		t.Fatalf("legacy lesson: %+v, %v", got, err)
	}

	if _, err := p.DownTo(ctx, lessonCallLinkVersion-1); err != nil {
		t.Fatalf("down to %d: %v", lessonCallLinkVersion-1, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM pragma_table_info('schedule_entries') WHERE name = 'call_link'"); n != 0 {
		t.Fatal("call_link still exists after down")
	}
}

// A stored link is untrusted on read: a row whose link breaks the rules is not restored, and the column refuses a
// value over the limit.
func TestLessonCallLinkCheckedOnRead(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	repo := NewLessonRepository(db)
	if err := repo.Save(ctx, newTestLesson(t, "l1", "ics:uid:1", 0)); err != nil {
		t.Fatal(err)
	}

	mustExec(t, db, `UPDATE schedule_entries SET call_link = 'javascript:alert(1)' WHERE id = 'l1'`)
	if _, err := repo.Get(ctx, "l1"); err == nil {
		t.Fatal("Get of a lesson with a bad stored link succeeded")
	}

	_, err := db.sql.ExecContext(ctx, `UPDATE schedule_entries SET call_link = 'https://example.com/' || printf('%.2040c', 'a') WHERE id = 'l1'`)
	if !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("over-long link: %v, want ErrConstraint", err)
	}
}
