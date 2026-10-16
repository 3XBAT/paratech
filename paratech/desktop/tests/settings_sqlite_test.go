package tests

// The SQLite settings repository behind the use case ports that consume it (TASK-007, ARCH-21): the writer port of
// modelcatalog and the reader ports of recordingsession and jobrunner. The STT handler (transcribe) reads no settings:
// its model comes from the job pin and the power switch is applied by the job runner.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/cases/recordingsession"
)

var (
	_ modelcatalog.SettingsRepository = (*sqlite.SettingsRepository)(nil)
	_ recordingsession.SettingsReader = (*sqlite.SettingsRepository)(nil)
	_ jobrunner.SettingsReader        = (*sqlite.SettingsRepository)(nil)
)

// withSettings is one run of the app: open the database, build the repository, close everything afterwards.
func withSettings(t *testing.T, path string, fn func(r *sqlite.SettingsRepository)) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()

	fn(sqlite.NewSettingsRepository(db))
}

// TestSettingsThroughPortsSurviveRestart writes through the writer port and reads through each reader port after the
// repository and the database were recreated (AC-02, AC-22).
func TestSettingsThroughPortsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "app.db")
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

	withSettings(t, path, func(r *sqlite.SettingsRepository) {
		var w modelcatalog.SettingsRepository = r
		s, err := w.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !s.RecordingPromptsEnabled() || !s.ProcessOnlyOnACPower() || s.Version() != 0 {
			t.Fatalf("empty database: prompts=%v acOnly=%v version=%d, want the defaults with version 0",
				s.RecordingPromptsEnabled(), s.ProcessOnlyOnACPower(), s.Version())
		}

		if err := s.SetRecordingPromptsEnabled(false, now); err != nil {
			t.Fatal(err)
		}
		if err := s.SetProcessOnlyOnACPower(false, now); err != nil {
			t.Fatal(err)
		}
		if err := w.Save(ctx, s); err != nil {
			t.Fatal(err)
		}
	})

	withSettings(t, path, func(r *sqlite.SettingsRepository) {
		var rec recordingsession.SettingsReader = r
		s, err := rec.Load(ctx)
		if err != nil || s.RecordingPromptsEnabled() {
			t.Errorf("recordingsession reader: prompts=%v err=%v, want prompts off (AC-02)", s != nil && s.RecordingPromptsEnabled(), err)
		}

		var jobs jobrunner.SettingsReader = r
		s, err = jobs.Load(ctx)
		if err != nil || s.ProcessOnlyOnACPower() {
			t.Errorf("jobrunner reader: acOnly=%v err=%v, want the restriction off (AC-22)", s != nil && s.ProcessOnlyOnACPower(), err)
		}

		if err == nil && s.Version() != 1 {
			t.Errorf("jobrunner reader: got %+v; want the stored row with version 1", s)
		}
	})
}
