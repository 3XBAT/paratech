package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
)

// TASK-294: photo recognition round-trips; a row stored without it reads as off; the column takes only off and local.
func TestSettingsNoteRecognitionRoundTrip(t *testing.T) {
	db := openTest(t, nil)
	r := NewSettingsRepository(db)

	if s := loadSettings(t, r); s.NoteRecognition() != settings.NoteRecognitionOff {
		t.Fatalf("defaults: %q, want off", s.NoteRecognition())
	}
	mustExec(t, db, "INSERT INTO settings (id, version, updated_at) VALUES (1, 1, 5)")
	s := loadSettings(t, r)
	if s.NoteRecognition() != settings.NoteRecognitionOff {
		t.Fatalf("a row stored without the value: %q, want off", s.NoteRecognition())
	}

	mustChange(t, s.SetNoteRecognition(settings.NoteRecognitionLocal, settingsT0.Add(time.Minute)))
	saveSettings(t, r, s)
	if got := loadSettings(t, r); got.NoteRecognition() != settings.NoteRecognitionLocal || got.Version() != 2 {
		t.Fatalf("after local: %q, version %d", got.NoteRecognition(), got.Version())
	}
	if v := scanString(t, db, "SELECT note_recognition FROM settings WHERE id = 1"); v != "local" {
		t.Fatalf("stored %q, want local", v)
	}

	s = loadSettings(t, r)
	mustChange(t, s.SetNoteRecognition(settings.NoteRecognitionOff, settingsT0.Add(2*time.Minute)))
	saveSettings(t, r, s)
	if got := loadSettings(t, r); got.NoteRecognition() != settings.NoteRecognitionOff {
		t.Fatalf("after off: %q", got.NoteRecognition())
	}

	for _, bad := range []string{"cloud", "LOCAL", ""} {
		_, err := db.sql.ExecContext(t.Context(), "UPDATE settings SET note_recognition = ? WHERE id = 1", bad)
		if !errors.Is(MapError(t.Context(), err), ErrConstraint) {
			t.Errorf("the column took %q: %v", bad, err)
		}
	}
}

// TASK-294: a stale version does not overwrite the setting.
func TestSettingsNoteRecognitionStaleVersion(t *testing.T) {
	db := openTest(t, nil)
	r := NewSettingsRepository(db)
	saveSettings(t, r, settings.New())

	first, second := loadSettings(t, r), loadSettings(t, r)
	mustChange(t, first.SetNoteRecognition(settings.NoteRecognitionLocal, settingsT0))
	saveSettings(t, r, first)
	mustChange(t, second.SetFreeWorkNotice(false, settingsT0))
	if err := r.Save(t.Context(), second); err == nil {
		t.Fatal("a stale version was saved")
	}
	if got := loadSettings(t, r); got.NoteRecognition() != settings.NoteRecognitionLocal {
		t.Fatalf("after the stale save: %q, want local", got.NoteRecognition())
	}
}

// Version 45 gives the settings row of an older database 'off' and keeps the rest; it goes back down and up again.
func TestMigrationNoteRecognition(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, noteRecognitionVersion-1); err != nil {
		t.Fatalf("up to %d: %v", noteRecognitionVersion-1, err)
	}
	mustExec(t, db, `INSERT INTO settings (id, recording_prompts_enabled, hub_url, free_work_notice, version, updated_at)
		VALUES (1, 0, 'https://hub.example', 0, 3, 9)`)

	if _, err := p.UpTo(ctx, noteRecognitionVersion); err != nil {
		t.Fatalf("migrate to %d: %v", noteRecognitionVersion, err)
	}
	if v := scanString(t, db, "SELECT note_recognition FROM settings WHERE id = 1"); v != "off" {
		t.Fatalf("existing row: %q, want off", v)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate to the latest: %v", err)
	}
	got, err := NewSettingsRepository(db).Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.NoteRecognition() != settings.NoteRecognitionOff || got.RecordingPromptsEnabled() || got.FreeWorkNotice() ||
		got.HubURL() != "https://hub.example" || got.Version() != 3 {
		t.Fatalf("settings after the migration: recognition %q, prompts %v, free work %v, hub %q, version %d",
			got.NoteRecognition(), got.RecordingPromptsEnabled(), got.FreeWorkNotice(), got.HubURL(), got.Version())
	}

	if _, err := p.DownTo(ctx, noteRecognitionVersion-1); err != nil {
		t.Fatalf("down to %d: %v", noteRecognitionVersion-1, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM pragma_table_info('settings') WHERE name = 'note_recognition'"); n != 0 {
		t.Fatal("note_recognition still exists after down")
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM settings"); n != 1 {
		t.Fatalf("settings rows after down = %d, want 1", n)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
