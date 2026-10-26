package sqlite

import (
	"testing"
	"time"
)

// TASK-188: the free-work card setting round-trips; a row stored before migration 0032 reads as on; the column takes
// only 0 and 1.
func TestSettingsFreeWorkNoticeRoundTrip(t *testing.T) {
	db := openTest(t, nil)
	r := NewSettingsRepository(db)

	if s := loadSettings(t, r); !s.FreeWorkNotice() {
		t.Fatal("defaults: the card is off")
	}
	mustExec(t, db, "INSERT INTO settings (id, version, updated_at) VALUES (1, 1, 5)")
	s := loadSettings(t, r)
	if !s.FreeWorkNotice() {
		t.Fatal("a row stored before the column reads as off")
	}

	mustChange(t, s.SetFreeWorkNotice(false, settingsT0.Add(time.Minute)))
	saveSettings(t, r, s)
	if got := loadSettings(t, r); got.FreeWorkNotice() || got.Version() != 2 {
		t.Fatalf("after off: on %v, version %d", got.FreeWorkNotice(), got.Version())
	}
	if n := scanInt(t, db.sql, "SELECT free_work_notice FROM settings WHERE id = 1"); n != 0 {
		t.Fatalf("stored %d, want 0", n)
	}

	s = loadSettings(t, r)
	mustChange(t, s.SetFreeWorkNotice(true, settingsT0.Add(2*time.Minute)))
	saveSettings(t, r, s)
	if got := loadSettings(t, r); !got.FreeWorkNotice() {
		t.Fatal("after on: off")
	}

	if _, err := db.sql.ExecContext(t.Context(), "UPDATE settings SET free_work_notice = 2 WHERE id = 1"); err == nil {
		t.Fatal("the column took 2")
	}
}
