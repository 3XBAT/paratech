package lesson

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

func content(title string, start time.Time, d time.Duration) Content {
	return Content{Title: title, Start: start, End: start.Add(d), TimeZone: "Europe/Moscow"}
}

func newLesson(t *testing.T) *Lesson {
	t.Helper()
	l, err := New(Spec{ID: "l1", Content: content("Math", t0, 90*time.Minute), SourceUID: "csv:id:1"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestContentValidate(t *testing.T) {
	ok := content("Math", t0, time.Hour)
	tests := []struct {
		name   string
		mutate func(*Content)
		valid  bool
	}{
		{"valid", func(*Content) {}, true},
		{"empty title", func(c *Content) { c.Title = "" }, false},
		{"blank title", func(c *Content) { c.Title = " \t" }, false},
		{"long title", func(c *Content) { c.Title = strings.Repeat("a", MaxTitleBytes+1) }, false},
		{"title at limit", func(c *Content) { c.Title = strings.Repeat("a", MaxTitleBytes) }, true},
		{"control char in title", func(c *Content) { c.Title = "Math\n2" }, false},
		{"invalid utf-8 title", func(c *Content) { c.Title = "Math\xff" }, false},
		{"zero start", func(c *Content) { c.Start = time.Time{} }, false},
		{"end equals start", func(c *Content) { c.End = c.Start }, false},
		{"end before start", func(c *Content) { c.End = c.Start.Add(-time.Minute) }, false},
		{"longer than a day", func(c *Content) { c.End = c.Start.Add(MaxDuration + time.Minute) }, false},
		{"exactly a day", func(c *Content) { c.End = c.Start.Add(MaxDuration) }, true},
		{"empty zone", func(c *Content) { c.TimeZone = "" }, false},
		{"utc", func(c *Content) { c.TimeZone = "UTC" }, true},
		{"etc offset zone", func(c *Content) { c.TimeZone = "Etc/GMT+3" }, true},
		{"deep zone", func(c *Content) { c.TimeZone = "America/Argentina/Buenos_Aires" }, true},
		{"zone with dots", func(c *Content) { c.TimeZone = "../etc/passwd" }, false},
		{"zone with space", func(c *Content) { c.TimeZone = "Europe/ Moscow" }, false},
		{"zone with leading slash", func(c *Content) { c.TimeZone = "/Europe" }, false},
		{"long zone", func(c *Content) { c.TimeZone = strings.Repeat("A", MaxTimeZoneBytes+1) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ok
			tt.mutate(&c)
			err := c.Validate()
			if tt.valid && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidLesson) {
				t.Fatalf("Validate() = %v, want ErrInvalidLesson", err)
			}
		})
	}
}

func TestContentEqualAndOverlaps(t *testing.T) {
	a := content("Math", t0, time.Hour)
	moscow := time.FixedZone("MSK", 3*3600)
	sameInstant := a
	sameInstant.Start, sameInstant.End = a.Start.In(moscow), a.End.In(moscow)
	if !a.Equal(sameInstant) {
		t.Error("same instants in another location are not Equal")
	}
	other := a
	other.TimeZone = "UTC"
	if a.Equal(other) {
		t.Error("another zone name is Equal")
	}

	tests := []struct {
		name string
		b    Content
		want bool
	}{
		{"same", a, true},
		{"inside", content("x", t0.Add(10*time.Minute), 10*time.Minute), true},
		{"straddles end", content("x", t0.Add(30*time.Minute), time.Hour), true},
		{"touches end", content("x", t0.Add(time.Hour), time.Hour), false},
		{"touches start", content("x", t0.Add(-time.Hour), time.Hour), false},
		{"apart", content("x", t0.Add(3*time.Hour), time.Hour), false},
	}
	for _, tt := range tests {
		if got := a.Overlaps(tt.b); got != tt.want || tt.b.Overlaps(a) != tt.want {
			t.Errorf("%s: Overlaps = %v, want %v (both ways)", tt.name, got, tt.want)
		}
	}
}

func TestNew(t *testing.T) {
	l := newLesson(t)
	if l.Version() != 0 || l.ContentRevision() != 1 || l.CalendarSyncedRevision() != 0 || l.Deleted() {
		t.Fatalf("new lesson: version %d, revision %d, synced %d, deleted %v", l.Version(), l.ContentRevision(),
			l.CalendarSyncedRevision(), l.Deleted())
	}
	if !l.CalendarPending() {
		t.Error("a lesson never written to the calendar is not pending")
	}
	if l.SourceUID() != "csv:id:1" || l.Content().Title != "Math" || !l.CreatedAt().Equal(t0) {
		t.Errorf("fields not kept: %+v", l)
	}

	bad := []struct {
		name string
		spec Spec
		now  time.Time
	}{
		{"no time", Spec{ID: "l1", Content: content("Math", t0, time.Hour)}, time.Time{}},
		{"bad id", Spec{ID: "a/b", Content: content("Math", t0, time.Hour)}, t0},
		{"empty id", Spec{Content: content("Math", t0, time.Hour)}, t0},
		{"bad content", Spec{ID: "l1", Content: content("", t0, time.Hour)}, t0},
		{"bad uid", Spec{ID: "l1", Content: content("Math", t0, time.Hour), SourceUID: "a\x00b"}, t0},
		{"long uid", Spec{ID: "l1", Content: content("Math", t0, time.Hour), SourceUID: strings.Repeat("u", MaxSourceUIDBytes+1)}, t0},
	}
	for _, tt := range bad {
		if _, err := New(tt.spec, tt.now); !errors.Is(err, ErrInvalidLesson) {
			t.Errorf("%s: New error = %v, want ErrInvalidLesson", tt.name, err)
		}
	}
}

func validState() State {
	return State{
		Version: 3, ID: "l1", Content: content("Math", t0, time.Hour), SourceUID: "csv:id:1",
		ContentRevision: 2, CalendarSyncedRevision: 2, CalendarSyncedAt: t0, CreatedAt: t0, UpdatedAt: t0,
	}
}

func TestRestore(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
		valid  bool
	}{
		{"valid", func(*State) {}, true},
		{"manual lesson", func(s *State) { s.SourceUID = "" }, true},
		{"deleted", func(s *State) { s.DeletedAt = t0 }, true},
		{"never synced", func(s *State) { s.CalendarSyncedRevision = 0; s.CalendarSyncedAt = time.Time{} }, true},
		{"notified", func(s *State) { s.StartNotifiedAt = t0 }, true},
		{"version 0", func(s *State) { s.Version = 0 }, false},
		{"revision 0", func(s *State) { s.ContentRevision = 0; s.CalendarSyncedRevision = 0 }, false},
		{"synced ahead of content", func(s *State) { s.CalendarSyncedRevision = 3 }, false},
		{"negative synced", func(s *State) { s.CalendarSyncedRevision = -1 }, false},
		{"bad content", func(s *State) { s.Content.End = s.Content.Start }, false},
		{"bad id", func(s *State) { s.ID = "" }, false},
		{"no times", func(s *State) { s.CreatedAt = time.Time{} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validState()
			tt.mutate(&s)
			l, err := Restore(s)
			if !tt.valid {
				if !errors.Is(err, ErrInvalidLesson) {
					t.Fatalf("Restore error = %v, want ErrInvalidLesson", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := State{
				Version: l.Version(), ID: l.ID(), Content: l.Content(), SourceUID: l.SourceUID(),
				StartNotifiedAt: l.StartNotifiedAt(), ContentRevision: l.ContentRevision(),
				CalendarSyncedRevision: l.CalendarSyncedRevision(), CalendarSyncedAt: l.CalendarSyncedAt(),
				DeletedAt: l.DeletedAt(), CreatedAt: l.CreatedAt(), UpdatedAt: l.UpdatedAt(),
			}
			if got != s {
				t.Fatalf("round trip:\n got %+v\nwant %+v", got, s)
			}
		})
	}
}

func TestChangeContent(t *testing.T) {
	t1 := t0.Add(time.Hour)

	t.Run("same content is no change", func(t *testing.T) {
		l := newLesson(t)
		changed, err := l.ChangeContent(l.Content(), t1)
		if err != nil || changed || l.ContentRevision() != 1 || !l.UpdatedAt().Equal(t0) {
			t.Fatalf("changed=%v err=%v revision=%d updated=%v", changed, err, l.ContentRevision(), l.UpdatedAt())
		}
	})

	t.Run("real change bumps the revision", func(t *testing.T) {
		l := newLesson(t)
		c := l.Content()
		c.Title = "Physics"
		changed, err := l.ChangeContent(c, t1)
		if err != nil || !changed || l.ContentRevision() != 2 || !l.UpdatedAt().Equal(t1) || l.Content().Title != "Physics" {
			t.Fatalf("changed=%v err=%v revision=%d updated=%v", changed, err, l.ContentRevision(), l.UpdatedAt())
		}
	})

	t.Run("invalid content is refused", func(t *testing.T) {
		l := newLesson(t)
		c := l.Content()
		c.End = c.Start
		if _, err := l.ChangeContent(c, t1); !errors.Is(err, ErrInvalidLesson) {
			t.Fatalf("err = %v, want ErrInvalidLesson", err)
		}
		if l.ContentRevision() != 1 || !l.Content().End.After(l.Content().Start) {
			t.Fatal("a refused change modified the lesson")
		}
	})

	t.Run("no time", func(t *testing.T) {
		if _, err := newLesson(t).ChangeContent(content("x", t0, time.Hour), time.Time{}); !errors.Is(err, ErrInvalidLesson) {
			t.Fatalf("err = %v, want ErrInvalidLesson", err)
		}
	})

	t.Run("deleted lesson", func(t *testing.T) {
		l := newLesson(t)
		if err := l.MarkDeleted(t1); err != nil {
			t.Fatal(err)
		}
		if _, err := l.ChangeContent(content("x", t0, time.Hour), t1); !errors.Is(err, ErrDeleted) {
			t.Fatalf("err = %v, want ErrDeleted", err)
		}
	})
}

func TestMarkDeleted(t *testing.T) {
	t1 := t0.Add(time.Hour)
	l := newLesson(t)
	if err := l.MarkDeleted(time.Time{}); !errors.Is(err, ErrInvalidLesson) || l.Deleted() {
		t.Fatalf("MarkDeleted(zero) = %v, deleted %v", err, l.Deleted())
	}
	if err := l.MarkDeleted(t1); err != nil {
		t.Fatal(err)
	}
	if !l.Deleted() || !l.DeletedAt().Equal(t1) || l.ContentRevision() != 2 || !l.UpdatedAt().Equal(t1) {
		t.Fatalf("deleted %v at %v, revision %d", l.Deleted(), l.DeletedAt(), l.ContentRevision())
	}
	if err := l.MarkDeleted(t1); !errors.Is(err, ErrDeleted) || l.ContentRevision() != 2 {
		t.Fatalf("second MarkDeleted = %v (revision %d), want ErrDeleted and no change", err, l.ContentRevision())
	}
}

func TestCalendarPending(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
		want   bool
	}{
		{"synced at current revision", func(*State) {}, false},
		{"never synced", func(s *State) { s.CalendarSyncedRevision = 0 }, true},
		{"changed after sync", func(s *State) { s.CalendarSyncedRevision = 1 }, true},
		{"deleted after sync", func(s *State) { s.DeletedAt = t0 }, true},
	}
	for _, tt := range tests {
		s := validState()
		tt.mutate(&s)
		l, err := Restore(s)
		if err != nil {
			t.Fatal(err)
		}
		if got := l.CalendarPending(); got != tt.want {
			t.Errorf("%s: CalendarPending = %v, want %v", tt.name, got, tt.want)
		}
	}

	l, err := Restore(validState())
	if err != nil {
		t.Fatal(err)
	}
	c := l.Content()
	c.Title = "Physics"
	if _, err := l.ChangeContent(c, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !l.CalendarPending() {
		t.Error("a synced lesson is not pending after a content change")
	}
}
