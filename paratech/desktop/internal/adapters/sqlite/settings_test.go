package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

var settingsT0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

// settingsView is every value of the aggregate, so tests compare the whole state at once.
type settingsView struct {
	version                 int64
	prompts, acOnly, notify bool
	model                   shared.SttModelID
	updatedAt               time.Time
}

func viewOf(s *settings.Settings) settingsView {
	return settingsView{
		version: s.Version(), prompts: s.RecordingPromptsEnabled(), acOnly: s.ProcessOnlyOnACPower(),
		notify: s.NotifyOnProcessingComplete(), model: s.DefaultSttModel(), updatedAt: s.UpdatedAt(),
	}
}

func loadSettings(t *testing.T, r *SettingsRepository) *settings.Settings {
	t.Helper()
	s, err := r.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func saveSettings(t *testing.T, r *SettingsRepository, s *settings.Settings) {
	t.Helper()
	if err := r.Save(context.Background(), s); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func mustChange(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("change: %v", err)
	}
}

func TestSettingsLoadDefaultsOnEmptyDatabase(t *testing.T) {
	db := openTest(t, nil)
	r := NewSettingsRepository(db)

	got := viewOf(loadSettings(t, r))
	want := settingsView{prompts: true, acOnly: true, notify: true}
	if got != want {
		t.Fatalf("Load on an empty database = %+v, want the defaults %+v", got, want)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM settings"); n != 0 {
		t.Fatalf("Load wrote %d rows, want a pure read", n)
	}
}

func TestSettingsSave(t *testing.T) {
	ctx := context.Background()
	t1 := settingsT0.Add(time.Minute)

	tests := []struct {
		name    string
		seed    bool // store the defaults first
		change  func(*settings.Settings) error
		want    settingsView
		wantErr error
	}{
		{
			name:   "first save stores the defaults with version 1",
			change: func(*settings.Settings) error { return nil },
			want:   settingsView{version: 1, prompts: true, acOnly: true, notify: true},
		},
		{
			name:   "first save of a change",
			change: func(s *settings.Settings) error { return s.SetNotifyOnProcessingComplete(false, t1) },
			want:   settingsView{version: 1, prompts: true, acOnly: true, updatedAt: t1},
		},
		{
			name:   "update of a stored row",
			seed:   true,
			change: func(s *settings.Settings) error { return s.SetRecordingPromptsEnabled(false, t1) },
			want:   settingsView{version: 2, acOnly: true, notify: true, updatedAt: t1},
		},
		{
			name:   "installed default model",
			seed:   true,
			change: func(s *settings.Settings) error { return s.SelectDefaultSttModel("m1", t1) },
			want:   settingsView{version: 2, prompts: true, acOnly: true, notify: true, model: "m1", updatedAt: t1},
		},
		{
			name:    "default model that is not installed is a conflict",
			seed:    true,
			change:  func(s *settings.Settings) error { return s.SelectDefaultSttModel("missing", t1) },
			want:    settingsView{version: 1, prompts: true, acOnly: true, notify: true},
			wantErr: shared.ErrConflict,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openTest(t, nil)
			mustExec(t, db, insertModelSQL(), "m1", "small", "1")
			r := NewSettingsRepository(db)
			if tc.seed {
				saveSettings(t, r, settings.New())
			}

			s := loadSettings(t, r)
			before := s.Version()
			mustChange(t, tc.change(s))

			err := r.Save(ctx, s)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || errors.Is(err, shared.ErrVersionConflict) {
					t.Fatalf("Save = %v, want %v", err, tc.wantErr)
				}
				if s.Version() != before {
					t.Fatalf("a failed Save moved the version to %d", s.Version())
				}
			} else if err != nil {
				t.Fatalf("Save: %v", err)
			}

			if got := viewOf(loadSettings(t, r)); got != tc.want {
				t.Fatalf("stored = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSettingsSaveSameInstanceAgain(t *testing.T) {
	r := NewSettingsRepository(openTest(t, nil))
	s := loadSettings(t, r)

	for i := range 3 {
		mustChange(t, s.SetProcessOnlyOnACPower(i%2 == 0, settingsT0.Add(time.Duration(i+1)*time.Second)))
		saveSettings(t, r, s)
		if s.Version() != int64(i+1) {
			t.Fatalf("save %d: Version() = %d, want %d", i, s.Version(), i+1)
		}
	}
	if got := loadSettings(t, r); got.Version() != 3 || !got.ProcessOnlyOnACPower() {
		t.Fatalf("stored = %+v", viewOf(got))
	}
}

func TestSettingsStaleSaveIsVersionConflict(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		setup func(t *testing.T, r *SettingsRepository) (stale *settings.Settings)
	}{
		{
			name: "two loads of the stored row, the second save loses",
			setup: func(t *testing.T, r *SettingsRepository) *settings.Settings {
				saveSettings(t, r, settings.New())
				first, second := loadSettings(t, r), loadSettings(t, r)
				mustChange(t, first.SetRecordingPromptsEnabled(false, settingsT0))
				saveSettings(t, r, first)
				mustChange(t, second.SetNotifyOnProcessingComplete(false, settingsT0))
				return second
			},
		},
		{
			name: "two first saves of the defaults, the second one loses",
			setup: func(t *testing.T, r *SettingsRepository) *settings.Settings {
				first, second := loadSettings(t, r), loadSettings(t, r)
				saveSettings(t, r, first)
				mustChange(t, second.SetNotifyOnProcessingComplete(false, settingsT0))
				return second
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openTest(t, nil)
			r := NewSettingsRepository(db)
			stale := tc.setup(t, r)
			before := viewOf(loadSettings(t, r))
			staleVersion := stale.Version()

			err := r.Save(ctx, stale)
			if !errors.Is(err, shared.ErrVersionConflict) || errors.Is(err, shared.ErrDuplicate) {
				t.Fatalf("stale Save = %v, want ErrVersionConflict", err)
			}
			if stale.Version() != staleVersion {
				t.Fatalf("a failed Save moved the version from %d to %d", staleVersion, stale.Version())
			}
			if got := viewOf(loadSettings(t, r)); got != before {
				t.Fatalf("a losing Save changed the row: %+v, want %+v", got, before)
			}
		})
	}
}

func TestSettingsConcurrentUpdatesLoseNothing(t *testing.T) {
	const writers = 8
	r := NewSettingsRepository(openTest(t, nil))
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Go(func() {
			// Each writer retries on a version conflict, as a use case does: reload and decide again.
			for {
				s, err := r.Load(ctx)
				if err != nil {
					errs <- err
					return
				}
				if err := s.SetRecordingPromptsEnabled(i%2 == 0, settingsT0.Add(time.Duration(s.Version()+1)*time.Second)); err != nil {
					errs <- err
					return
				}
				err = r.Save(ctx, s)
				if errors.Is(err, shared.ErrVersionConflict) {
					continue
				}
				errs <- err
				return
			}
		})
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("writer: %v", err)
		}
	}
	// Writers that set the stored value again do not change UpdatedAt but still bump the version.
	if got := loadSettings(t, r).Version(); got != writers {
		t.Fatalf("version = %d after %d successful saves", got, writers)
	}
}

func TestSettingsSaveRolledBackWithTheOuterTransaction(t *testing.T) {
	db := openTest(t, nil)
	r := NewSettingsRepository(db)
	ctx := context.Background()
	errAbort := errors.New("abort")

	err := db.Do(ctx, func(ctx context.Context) error {
		s := loadSettings(t, r)
		mustChange(t, s.SetProcessOnlyOnACPower(false, settingsT0))
		if err := r.Save(ctx, s); err != nil {
			return err
		}
		return errAbort
	})
	if !errors.Is(err, errAbort) {
		t.Fatalf("Do = %v, want the abort error", err)
	}

	if got := viewOf(loadSettings(t, r)); got != (settingsView{prompts: true, acOnly: true, notify: true}) {
		t.Fatalf("a rolled-back Save is visible: %+v", got)
	}
}

func TestSettingsLoadRejectsCorruptRow(t *testing.T) {
	db := openTest(t, nil)
	// Version 0 never exists in a stored row (ADR-0004(d)); only a hand-made row can have it.
	mustExec(t, db, `INSERT INTO settings (id, version, updated_at) VALUES (1, 0, 0)`)

	_, err := NewSettingsRepository(db).Load(context.Background())
	if !errors.Is(err, settings.ErrInvalidSettings) {
		t.Fatalf("Load = %v, want ErrInvalidSettings", err)
	}
}

func TestSettingsLoadAndSaveAfterClose(t *testing.T) {
	db := openTest(t, nil)
	r := NewSettingsRepository(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Load(context.Background()); !errors.Is(err, ErrClosed) {
		t.Errorf("Load after Close = %v, want ErrClosed", err)
	}
	if err := r.Save(context.Background(), settings.New()); !errors.Is(err, ErrClosed) {
		t.Errorf("Save after Close = %v, want ErrClosed", err)
	}
}

// reopenSettings is one run of the app: open the database at path, run fn, close it.
func reopenSettings(t *testing.T, path string, fn func(r *SettingsRepository)) {
	t.Helper()
	db, err := Open(context.Background(), Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Deferred so that a failing fn still closes the file; Windows cannot remove an open temp dir.
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	fn(NewSettingsRepository(db))
}

// TestSettingsPersistAcrossRestarts covers AC-02 (prompts off and on again) and AC-22 (AC-power restriction off) at
// the storage level: every change survives closing and reopening the database.
func TestSettingsPersistAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.db")
	steps := []struct {
		name   string
		change func(*settings.Settings, time.Time) error
		want   settingsView // version and updatedAt are checked separately
	}{
		{
			name:   "AC-02: disable recording prompts",
			change: func(s *settings.Settings, now time.Time) error { return s.SetRecordingPromptsEnabled(false, now) },
			want:   settingsView{acOnly: true, notify: true},
		},
		{
			name:   "AC-22: disable the AC-power restriction",
			change: func(s *settings.Settings, now time.Time) error { return s.SetProcessOnlyOnACPower(false, now) },
			want:   settingsView{notify: true},
		},
		{
			name:   "AC-02: re-enable recording prompts",
			change: func(s *settings.Settings, now time.Time) error { return s.SetRecordingPromptsEnabled(true, now) },
			want:   settingsView{prompts: true, notify: true},
		},
		{
			name:   "AC-22: re-enable the AC-power restriction",
			change: func(s *settings.Settings, now time.Time) error { return s.SetProcessOnlyOnACPower(true, now) },
			want:   settingsView{prompts: true, acOnly: true, notify: true},
		},
		{
			name:   "disable notifications",
			change: func(s *settings.Settings, now time.Time) error { return s.SetNotifyOnProcessingComplete(false, now) },
			want:   settingsView{prompts: true, acOnly: true},
		},
	}

	reopenSettings(t, path, func(r *SettingsRepository) {
		if got := viewOf(loadSettings(t, r)); got != (settingsView{prompts: true, acOnly: true, notify: true}) {
			t.Fatalf("fresh database = %+v, want the defaults", got)
		}
	})
	for i, step := range steps {
		now := settingsT0.Add(time.Duration(i+1) * time.Minute)
		reopenSettings(t, path, func(r *SettingsRepository) {
			s := loadSettings(t, r)
			mustChange(t, step.change(s, now))
			saveSettings(t, r, s)
		})

		reopenSettings(t, path, func(r *SettingsRepository) {
			want := step.want
			want.version, want.updatedAt = int64(i+1), now
			if got := viewOf(loadSettings(t, r)); got != want {
				t.Fatalf("%s: after reopen = %+v, want %+v", step.name, got, want)
			}
		})
	}
}
