package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
)

// TASK-094: the student profile survives a save and a reload, other settings writes keep it, and the columns refuse an
// oversized text; a stored profile the aggregate refuses is dropped, not a failed load.
func TestSettingsProfileRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	r := NewSettingsRepository(db)

	want := settings.ProfileFields{
		FullName: "Иванов Иван Иванович", Group: "ИВТ-21", University: "МГУ им. М. В. Ломоносова",
		Faculty: "Факультет ВМК", City: "Москва",
	}
	p, err := settings.NewProfile(want)
	if err != nil {
		t.Fatal(err)
	}
	s := loadSettings(t, r)
	mustChange(t, s.SetProfile(p, settingsT0))
	saveSettings(t, r, s)

	got := loadSettings(t, r)
	if got.Profile().Fields() != want {
		t.Fatalf("reloaded profile = %+v", got.Profile().Fields())
	}
	mustChange(t, got.SetProcessOnlyOnACPower(false, settingsT0.Add(time.Minute)))
	saveSettings(t, r, got)
	if again := loadSettings(t, r); again.Profile().Fields() != want {
		t.Fatalf("another setting dropped the profile: %+v", again.Profile().Fields())
	}

	if _, err := db.sql.ExecContext(ctx, "UPDATE settings SET student_group = ?", strings.Repeat("x", 201)); !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("oversized group = %v, want a constraint violation", err)
	}
	mustExec(t, db, "UPDATE settings SET student_full_name = 'Иванов' || char(10) || 'Иван'")
	broken := loadSettings(t, r)
	if !broken.Profile().IsZero() {
		t.Fatalf("a profile with a line break was loaded: %+v", broken.Profile().Fields())
	}
}
