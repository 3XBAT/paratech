package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

func mustSpeed(t *testing.T, factor float64, samples int) values.MeasuredSpeed {
	t.Helper()
	s, err := values.RestoreMeasuredSpeed(factor, samples)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TASK-084: the measured speed is stored per model and mode, replaced on save, and goes with the model.
func TestSpeedStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	models := NewModelRepository(db)
	saveModel(t, models, newModel(t, "m-1", "small", "1"))
	saveModel(t, models, newModel(t, "m-2", "medium", "1"))
	r := NewSpeedStore(db)

	gentle := values.SpeedKey{Model: "m-1", Load: shared.LoadGentle}
	if s, err := r.Speed(ctx, gentle); err != nil || !s.IsZero() {
		t.Fatalf("nothing stored: %+v, %v", s, err)
	}

	if err := r.SaveSpeed(ctx, gentle, mustSpeed(t, 0.5, 1), modelT0); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveSpeed(ctx, gentle, mustSpeed(t, 0.65, 2), modelT0); err != nil {
		t.Fatal(err)
	}
	maxKey := values.SpeedKey{Model: "m-2", Load: shared.LoadMax}
	if err := r.SaveSpeed(ctx, maxKey, mustSpeed(t, 0.2, 1), modelT0); err != nil {
		t.Fatal(err)
	}

	if s, err := r.Speed(ctx, gentle); err != nil || s != mustSpeed(t, 0.65, 2) {
		t.Fatalf("Speed = %+v, %v; want the replaced 0.65 over 2", s, err)
	}
	all, err := r.Speeds(ctx)
	if err != nil || len(all) != 2 || all[gentle] != mustSpeed(t, 0.65, 2) || all[maxKey] != mustSpeed(t, 0.2, 1) {
		t.Fatalf("Speeds = %v, %v", all, err)
	}

	if err := models.Delete(ctx, "m-1"); err != nil {
		t.Fatal(err)
	}
	if all, err := r.Speeds(ctx); err != nil || len(all) != 1 {
		t.Fatalf("after the model is deleted: %v, %v; want only m-2", all, err)
	}
}

func TestSpeedStoreRefusesBadInput(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	saveModel(t, NewModelRepository(db), newModel(t, "m-1", "small", "1"))
	r := NewSpeedStore(db)

	if err := r.SaveSpeed(ctx, values.SpeedKey{Model: "gone", Load: shared.LoadGentle}, mustSpeed(t, 0.5, 1), modelT0); !errors.Is(err, shared.ErrConflict) {
		t.Errorf("unknown model: %v, want ErrConflict", err)
	}
	if err := r.SaveSpeed(ctx, values.SpeedKey{Model: "m-1", Load: "turbo"}, mustSpeed(t, 0.5, 1), modelT0); err == nil {
		t.Error("unknown mode stored")
	}
	if err := r.SaveSpeed(ctx, values.SpeedKey{Model: "m-1", Load: shared.LoadGentle}, values.MeasuredSpeed{}, modelT0); err == nil {
		t.Error("a zero speed stored")
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO stt_speeds VALUES ('m-1', 'turbo', 0.5, 1, 0)`); !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Errorf("unknown mode by SQL = %v, want a constraint violation", err)
	}

	// A tampered row reads as not measured instead of failing the pages.
	mustExec(t, db, `INSERT INTO stt_speeds VALUES ('m-1', 'normal', 1000, 1, 0)`)
	key := values.SpeedKey{Model: "m-1", Load: shared.LoadNormal}
	if s, err := r.Speed(ctx, key); err != nil || !s.IsZero() {
		t.Errorf("tampered Speed = %+v, %v; want not measured", s, err)
	}
	if all, err := r.Speeds(ctx); err != nil || len(all) != 0 {
		t.Errorf("tampered Speeds = %v, %v; want none", all, err)
	}
}

// TASK-084: the load mode survives a restart, and a tampered value reads as the default.
func TestSettingsRecognitionLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.db")
	reopenSettings(t, path, func(r *SettingsRepository) {
		s := loadSettings(t, r)
		if s.RecognitionLoad() != shared.LoadGentle {
			t.Fatalf("fresh database mode %q, want gentle", s.RecognitionLoad())
		}
		mustChange(t, s.SetRecognitionLoad(shared.LoadMax, settingsT0))
		saveSettings(t, r, s)
	})
	reopenSettings(t, path, func(r *SettingsRepository) {
		s := loadSettings(t, r)
		if s.RecognitionLoad() != shared.LoadMax {
			t.Fatalf("after reopen: mode %q, want max", s.RecognitionLoad())
		}
		mustChange(t, s.SetRecognitionLoad(shared.LoadNormal, settingsT0))
		saveSettings(t, r, s)
		if got := loadSettings(t, r).RecognitionLoad(); got != shared.LoadNormal {
			t.Fatalf("after update: mode %q, want normal", got)
		}
	})

	db := openTest(t, nil)
	r := NewSettingsRepository(db)
	saveSettings(t, r, settings.New())
	mustExec(t, db, `UPDATE settings SET recognition_load = 'turbo'`)
	if got := loadSettings(t, r).RecognitionLoad(); got != shared.LoadGentle {
		t.Errorf("tampered mode reads as %q, want gentle", got)
	}
	if _, err := db.sql.ExecContext(context.Background(), "UPDATE settings SET recognition_load = ?", "a-very-long-mode-name"); !errors.Is(MapError(context.Background(), err), ErrConstraint) {
		t.Errorf("an oversized mode = %v, want a constraint violation", err)
	}
}

// Version 11 adds the load mode with the default gentle to an existing settings row and the speed table, and goes
// back down to 10.
func TestMigrationToVersion11AddsRecognitionLoad(t *testing.T) {
	t.Parallel() // migrates a database of its own for real
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 10); err != nil {
		t.Fatalf("up to 10: %v", err)
	}
	mustExec(t, db, `INSERT INTO settings (id, recording_prompts_enabled, process_only_on_ac_power, recording_source_app,
		version, updated_at) VALUES (1, 0, 1, 'zoom.exe', 4, 9)`)

	if _, err := p.UpTo(ctx, 11); err != nil {
		t.Fatalf("migrate to 11: %v", err)
	}
	// The repository reads the columns of the later migrations too (TASK-094: the student profile).
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("migrate to the latest: %v", err)
	}
	got, err := NewSettingsRepository(db).Load(ctx)
	if err != nil {
		t.Fatalf("Load of a version 10 row: %v", err)
	}
	if got.RecognitionLoad() != shared.LoadGentle || got.RecordingSource() != "zoom.exe" || got.Version() != 4 {
		t.Fatalf("settings after the migration: mode %q, source %q, version %d", got.RecognitionLoad(),
			got.RecordingSource(), got.Version())
	}

	if _, err := p.DownTo(ctx, 10); err != nil {
		t.Fatalf("down to 10: %v", err)
	}
	const (
		speedsTable = "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'stt_speeds'"
		loadColumn  = "SELECT count(*) FROM pragma_table_info('settings') WHERE name = 'recognition_load'"
	)
	if scanInt(t, db.sql, speedsTable) != 0 || scanInt(t, db.sql, loadColumn) != 0 {
		t.Fatal("down to 10 left the speed table or the setting behind")
	}
}

// TASK-222: speeds measured before voice activity detection are twice too slow; version 37 removes them, the next
// runs measure again. Down and up again works on the empty table.
func TestMigrationToVersion37ResetsTheSpeeds(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 36); err != nil {
		t.Fatalf("up to 36: %v", err)
	}
	saveModel(t, NewModelRepository(db), newModel(t, "m-1", "small", "1"))
	r := NewSpeedStore(db)
	key := values.SpeedKey{Model: "m-1", Load: shared.LoadGentle}
	if err := r.SaveSpeed(ctx, key, mustSpeed(t, 0.92, 3), modelT0); err != nil {
		t.Fatal(err)
	}

	if _, err := p.UpTo(ctx, 37); err != nil {
		t.Fatalf("up to 37: %v", err)
	}
	if all, err := r.Speeds(ctx); err != nil || len(all) != 0 {
		t.Fatalf("speeds after the reset: %v, %v; want none", all, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM stt_models"); n != 1 {
		t.Fatalf("%d models after the reset, want the model kept", n)
	}

	if _, err := p.DownTo(ctx, 36); err != nil {
		t.Fatalf("down to 36: %v", err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if err := r.SaveSpeed(ctx, key, mustSpeed(t, 0.47, 1), modelT0); err != nil {
		t.Fatalf("a new measurement after the reset: %v", err)
	}
}
