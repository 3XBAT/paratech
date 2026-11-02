package sqlite

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/settings/settings"
)

const freeWorkColumn = "SELECT count(*) FROM pragma_table_info('settings') WHERE name = 'free_work_notice'"

// TASK-342: version 48 drops the free-work notice from a database of version 47 that holds it turned off, keeps the
// other settings (the repository loads and saves the row), and goes back down to 47 with the column of 0032.
func TestMigrationDropsFreeWorkNotice(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, dropFreeWorkNoticeVersion-1); err != nil {
		t.Fatalf("up to %d: %v", dropFreeWorkNoticeVersion-1, err)
	}
	mustExec(t, db, `INSERT INTO settings (id, recording_prompts_enabled, process_only_on_ac_power, hub_url,
		recognition_load, free_work_notice, note_recognition, version, updated_at)
		VALUES (1, 0, 1, 'https://hub.example', 'max', 0, 'local', 3, 9)`)

	if _, err := p.UpTo(ctx, dropFreeWorkNoticeVersion); err != nil {
		t.Fatalf("migrate to %d: %v", dropFreeWorkNoticeVersion, err)
	}
	if n := scanInt(t, db.sql, freeWorkColumn); n != 0 {
		t.Fatal("settings.free_work_notice still exists")
	}
	r := NewSettingsRepository(db)
	got, err := r.Load(ctx)
	if err != nil {
		t.Fatalf("Load of a version 47 row: %v", err)
	}
	if got.RecordingPromptsEnabled() || !got.ProcessOnlyOnACPower() || got.HubURL() != "https://hub.example" ||
		got.RecognitionLoad() != "max" || got.NoteRecognition() != settings.NoteRecognitionLocal || got.Version() != 3 {
		t.Fatalf("settings after the migration: prompts %v, AC only %v, hub %q, load %q, recognition %q, version %d",
			got.RecordingPromptsEnabled(), got.ProcessOnlyOnACPower(), got.HubURL(), got.RecognitionLoad(),
			got.NoteRecognition(), got.Version())
	}
	mustChange(t, got.SetRecordingPromptsEnabled(true, settingsT0))
	saveSettings(t, r, got)
	if s := loadSettings(t, r); !s.RecordingPromptsEnabled() || s.Version() != 4 {
		t.Fatalf("after a save: prompts %v, version %d", s.RecordingPromptsEnabled(), s.Version())
	}

	if _, err := p.DownTo(ctx, dropFreeWorkNoticeVersion-1); err != nil {
		t.Fatalf("down to %d: %v", dropFreeWorkNoticeVersion-1, err)
	}
	if n := scanInt(t, db.sql, "SELECT free_work_notice FROM settings WHERE id = 1"); n != 1 {
		t.Fatalf("restored column = %d, want the default 1", n)
	}
	if _, err := db.sql.ExecContext(ctx, "UPDATE settings SET free_work_notice = 2"); !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("restored CHECK: err = %v, want ErrConstraint", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if n := scanInt(t, db.sql, freeWorkColumn); n != 0 {
		t.Fatal("settings.free_work_notice is back after up again")
	}
}
