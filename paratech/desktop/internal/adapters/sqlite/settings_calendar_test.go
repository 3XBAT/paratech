package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
)

// TASK-276: the calendar mode round-trips; a row stored without it reads as ask; the column takes only the three modes.
func TestSettingsCalendarOnDueChangeRoundTrip(t *testing.T) {
	db := openTest(t, nil)
	r := NewSettingsRepository(db)

	if s := loadSettings(t, r); s.CalendarOnDueChange() != settings.CalendarAsk {
		t.Fatalf("defaults: %q, want ask", s.CalendarOnDueChange())
	}
	mustExec(t, db, "INSERT INTO settings (id, version, updated_at) VALUES (1, 1, 5)")
	s := loadSettings(t, r)
	if s.CalendarOnDueChange() != settings.CalendarAsk {
		t.Fatalf("a row stored without the value: %q, want ask", s.CalendarOnDueChange())
	}

	for i, mode := range []settings.CalendarOnDueChange{settings.CalendarUpdate, settings.CalendarKeep, settings.CalendarAsk} {
		s = loadSettings(t, r)
		mustChange(t, s.SetCalendarOnDueChange(mode, settingsT0.Add(time.Duration(i+1)*time.Minute)))
		saveSettings(t, r, s)
		if got := loadSettings(t, r); got.CalendarOnDueChange() != mode || got.Version() != int64(i+2) {
			t.Fatalf("after %s: %q, version %d", mode, got.CalendarOnDueChange(), got.Version())
		}
		if v := scanString(t, db, "SELECT deadline_event_on_due_change FROM settings WHERE id = 1"); v != string(mode) {
			t.Fatalf("stored %q, want %s", v, mode)
		}
	}

	for _, bad := range []string{"always", "UPDATE", ""} {
		_, err := db.sql.ExecContext(t.Context(), "UPDATE settings SET deadline_event_on_due_change = ? WHERE id = 1", bad)
		if !errors.Is(MapError(t.Context(), err), ErrConstraint) {
			t.Errorf("the column took %q: %v", bad, err)
		}
	}
}

// TASK-276: a stale version does not overwrite the setting.
func TestSettingsCalendarOnDueChangeStaleVersion(t *testing.T) {
	db := openTest(t, nil)
	r := NewSettingsRepository(db)
	saveSettings(t, r, settings.New())

	first, second := loadSettings(t, r), loadSettings(t, r)
	mustChange(t, first.SetCalendarOnDueChange(settings.CalendarKeep, settingsT0))
	saveSettings(t, r, first)
	mustChange(t, second.SetCalendarOnDueChange(settings.CalendarUpdate, settingsT0))
	if err := r.Save(t.Context(), second); err == nil {
		t.Fatal("a stale version was saved")
	}
	if got := loadSettings(t, r); got.CalendarOnDueChange() != settings.CalendarKeep {
		t.Fatalf("after the stale save: %q, want keep", got.CalendarOnDueChange())
	}
}

// Version 51 gives the settings row of an older database 'ask', keeps the rest and adds the empty queue of updates;
// it goes back down and up again.
func TestMigrationDeadlineEventUpdates(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, deadlineEventUpdatesVersion-1); err != nil {
		t.Fatalf("up to %d: %v", deadlineEventUpdatesVersion-1, err)
	}
	mustExec(t, db, `INSERT INTO settings (id, recording_prompts_enabled, note_recognition, version, updated_at)
		VALUES (1, 0, 'local', 3, 9)`)

	if _, err := p.UpTo(ctx, deadlineEventUpdatesVersion); err != nil {
		t.Fatalf("migrate to %d: %v", deadlineEventUpdatesVersion, err)
	}
	if v := scanString(t, db, "SELECT deadline_event_on_due_change FROM settings WHERE id = 1"); v != "ask" {
		t.Fatalf("existing row: %q, want ask", v)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM deadline_event_updates"); n != 0 {
		t.Fatalf("updates after the migration = %d, want 0", n)
	}
	got, err := NewSettingsRepository(db).Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.CalendarOnDueChange() != settings.CalendarAsk || got.RecordingPromptsEnabled() ||
		got.NoteRecognition() != settings.NoteRecognitionLocal || got.Version() != 3 {
		t.Fatalf("settings after the migration: calendar %q, prompts %v, recognition %q, version %d",
			got.CalendarOnDueChange(), got.RecordingPromptsEnabled(), got.NoteRecognition(), got.Version())
	}

	if _, err := p.DownTo(ctx, deadlineEventUpdatesVersion-1); err != nil {
		t.Fatalf("down to %d: %v", deadlineEventUpdatesVersion-1, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM pragma_table_info('settings') WHERE name = 'deadline_event_on_due_change'"); n != 0 {
		t.Fatal("deadline_event_on_due_change still exists after down")
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM sqlite_master WHERE name = 'deadline_event_updates'"); n != 0 {
		t.Fatal("deadline_event_updates still exists after down")
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM settings"); n != 1 {
		t.Fatalf("settings rows after down = %d, want 1", n)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
