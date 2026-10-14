package lesson

import (
	"errors"
	"testing"
	"time"
)

func TestValidateStartLead(t *testing.T) {
	tests := []struct {
		lead  time.Duration
		valid bool
	}{
		{0, true},
		{5 * time.Minute, true},
		{MaxStartLead, true},
		{MaxStartLead + time.Nanosecond, false},
		{-time.Nanosecond, false},
	}
	for _, tt := range tests {
		err := ValidateStartLead(tt.lead)
		if tt.valid != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidLead)) {
			t.Errorf("ValidateStartLead(%v) = %v, want valid %v", tt.lead, err, tt.valid)
		}
	}
}

// The lesson of newLesson runs from t0 to t0+90m; the window is [start-lead, start+StartLateGrace).
func TestStartAnnouncementDue(t *testing.T) {
	const lead = 10 * time.Minute
	tests := []struct {
		name string
		now  time.Time
		lead time.Duration
		due  bool
	}{
		{"before the lead window", t0.Add(-lead - time.Nanosecond), lead, false},
		{"at start minus lead", t0.Add(-lead), lead, true},
		{"at start", t0, lead, true},
		{"19 s after start", t0.Add(19 * time.Second), lead, true},
		{"just before the grace ends", t0.Add(StartLateGrace - time.Nanosecond), lead, true},
		{"when the grace ends", t0.Add(StartLateGrace), lead, false},
		{"21 s after start", t0.Add(21 * time.Second), lead, false},
		{"in the middle", t0.Add(45 * time.Minute), lead, false},
		{"after the end", t0.Add(2 * time.Hour), lead, false},
		{"zero lead before start", t0.Add(-time.Nanosecond), 0, false},
		{"zero lead at start", t0, 0, true},
		{"zero lead 19 s after start", t0.Add(19 * time.Second), 0, true},
		{"zero lead 21 s after start", t0.Add(21 * time.Second), 0, false},
		{"invalid lead", t0, -time.Minute, false},
		{"zero time", time.Time{}, lead, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newLesson(t).StartAnnouncementDue(tt.now, tt.lead); got != tt.due {
				t.Fatalf("StartAnnouncementDue(%v, %v) = %v, want %v", tt.now, tt.lead, got, tt.due)
			}
		})
	}
}

func TestAnnounceStart(t *testing.T) {
	const lead = 10 * time.Minute
	at := t0.Add(-lead)

	t.Run("raises the event once", func(t *testing.T) {
		l := newLesson(t)
		ev, err := l.AnnounceStart(at, lead)
		if err != nil {
			t.Fatal(err)
		}
		c := l.Content()
		want := StartAnnounced{
			LessonID: l.ID(), Title: c.Title, Start: c.Start, End: c.End, TimeZone: c.TimeZone, AnnouncedAt: at,
		}
		if ev != want {
			t.Fatalf("event = %+v, want %+v", ev, want)
		}
		if !l.StartNotifiedAt().Equal(at) || !l.UpdatedAt().Equal(at) {
			t.Fatalf("notified %v updated %v, want %v", l.StartNotifiedAt(), l.UpdatedAt(), at)
		}
		if l.ContentRevision() != 1 {
			t.Fatalf("content revision = %d: the prompt is not part of the calendar projection", l.ContentRevision())
		}
		if l.StartAnnouncementDue(at.Add(time.Minute), lead) {
			t.Fatal("still due after the announcement")
		}
		if _, err := l.AnnounceStart(at.Add(time.Minute), lead); !errors.Is(err, ErrAlreadyAnnounced) {
			t.Fatalf("second AnnounceStart = %v, want ErrAlreadyAnnounced", err)
		}
	})

	t.Run("a restored announced lesson stays silent", func(t *testing.T) {
		s := validState()
		s.StartNotifiedAt = s.Content.Start
		l, err := Restore(s)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.AnnounceStart(s.Content.Start.Add(time.Minute), lead); !errors.Is(err, ErrAlreadyAnnounced) {
			t.Fatalf("AnnounceStart = %v, want ErrAlreadyAnnounced", err)
		}
	})

	t.Run("a moved lesson is due again", func(t *testing.T) {
		l := newLesson(t)
		if _, err := l.AnnounceStart(at, lead); err != nil {
			t.Fatal(err)
		}
		moved := content("Math", t0.Add(3*time.Hour), 90*time.Minute)
		if _, err := l.ChangeContent(moved, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := l.AnnounceStart(moved.Start.Add(-lead), lead); err != nil {
			t.Fatalf("AnnounceStart after a move = %v", err)
		}
	})

	refusals := []struct {
		name string
		now  time.Time
		lead time.Duration
		prep func(*Lesson) error
		want error
	}{
		{"too early", at.Add(-time.Second), lead, nil, ErrNotDue},
		{"after the grace", t0.Add(StartLateGrace), lead, nil, ErrNotDue},
		{"mid-lesson", t0.Add(45 * time.Minute), lead, nil, ErrNotDue},
		{"over", t0.Add(90 * time.Minute), lead, nil, ErrNotDue},
		{"invalid lead", t0, MaxStartLead + time.Minute, nil, ErrInvalidLead},
		{"zero time", time.Time{}, lead, nil, ErrInvalidLesson},
		{"deleted", t0, lead, func(l *Lesson) error { return l.MarkDeleted(at) }, ErrDeleted},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			l := newLesson(t)
			if tt.prep != nil {
				if err := tt.prep(l); err != nil {
					t.Fatal(err)
				}
			}
			updated := l.UpdatedAt()
			if _, err := l.AnnounceStart(tt.now, tt.lead); !errors.Is(err, tt.want) {
				t.Fatalf("AnnounceStart = %v, want %v", err, tt.want)
			}
			if !l.StartNotifiedAt().IsZero() || !l.UpdatedAt().Equal(updated) {
				t.Fatal("a refused announcement changed the lesson")
			}
		})
	}
}

// The window is a pair of instants: a lesson stored at 09:00 New York time on the day daylight saving ends is due
// from 08:50 EST (13:50 UTC), not from 08:50 EDT (12:50 UTC).
func TestAnnounceStartAcrossZones(t *testing.T) {
	est := time.FixedZone("EST", -5*3600)
	start := time.Date(2026, 11, 1, 9, 0, 0, 0, est)
	l, err := New(Spec{ID: "ny", Content: Content{
		Title: "History", Start: start, End: start.Add(time.Hour), TimeZone: "America/New_York",
	}}, t0)
	if err != nil {
		t.Fatal(err)
	}

	lead := 10 * time.Minute
	if l.StartAnnouncementDue(time.Date(2026, 11, 1, 12, 50, 0, 0, time.UTC), lead) {
		t.Fatal("due an hour early (offset of the summer time)")
	}
	if !l.StartAnnouncementDue(time.Date(2026, 11, 1, 16, 50, 0, 0, time.FixedZone("MSK", 3*3600)), lead) {
		t.Fatal("not due at 13:50 UTC seen from another zone")
	}
	ev, err := l.AnnounceStart(time.Date(2026, 11, 1, 13, 50, 0, 0, time.UTC), lead)
	if err != nil || ev.TimeZone != "America/New_York" || !ev.Start.Equal(start) {
		t.Fatalf("event %+v, err %v", ev, err)
	}
}
