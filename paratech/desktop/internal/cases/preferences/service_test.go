package preferences

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// fakeRepo keeps the stored state and follows the compare-and-swap contract of the sqlite adapter.
type fakeRepo struct {
	stored    settings.State // Version 0: nothing stored
	conflicts int            // the next saves fail with a version conflict
	loadErr   error
	saves     int
}

func (r *fakeRepo) Load(context.Context) (*settings.Settings, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	if r.stored.Version == 0 {
		return settings.New(), nil
	}
	return settings.Restore(r.stored)
}

func (r *fakeRepo) Save(_ context.Context, s *settings.Settings) error {
	if r.conflicts > 0 {
		r.conflicts--
		r.stored.Version++ // another writer won
		return shared.ErrVersionConflict
	}
	if s.Version() != r.stored.Version {
		return shared.ErrVersionConflict
	}
	r.saves++
	r.stored = settings.State{
		Version:                 s.Version() + 1,
		RecordingPromptsEnabled: s.RecordingPromptsEnabled(),
		ProcessOnlyOnACPower:    s.ProcessOnlyOnACPower(),
		DefaultSttModel:         s.DefaultSttModel(),
		UpdatedAt:               s.UpdatedAt(),
	}
	s.Persisted(r.stored.Version)
	return nil
}

var start = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

func newService(t *testing.T, repo *fakeRepo) *Service {
	t.Helper()
	s, err := New(repo, testenv.NewFakeClock(start))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewRequiresPorts(t *testing.T) {
	if _, err := New(nil, testenv.NewFakeClock(start)); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("no repo: err = %v", err)
	}
	if _, err := New(&fakeRepo{}, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("no clock: err = %v", err)
	}
}

func TestGetReturnsDefaults(t *testing.T) {
	got, err := newService(t, &fakeRepo{}).Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Toggles{RecordingPromptsEnabled: settings.DefaultRecordingPromptsEnabled, ProcessOnlyOnACPower: settings.DefaultProcessOnlyOnACPower}
	if got != want {
		t.Fatalf("Get = %+v, want %+v", got, want)
	}
}

func TestSetToggles(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{}
	s := newService(t, repo)

	got, err := s.SetRecordingPromptsEnabled(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.RecordingPromptsEnabled || !got.ProcessOnlyOnACPower {
		t.Fatalf("after prompts off: %+v", got)
	}

	got, err = s.SetProcessOnlyOnACPower(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != (Toggles{}) {
		t.Fatalf("after AC off: %+v, want both off", got)
	}
	if !repo.stored.UpdatedAt.Equal(start) {
		t.Errorf("updated at = %v, want the clock time", repo.stored.UpdatedAt)
	}

	got, err = s.SetRecordingPromptsEnabled(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.RecordingPromptsEnabled || got.ProcessOnlyOnACPower {
		t.Fatalf("after prompts on again: %+v", got)
	}

	read, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if read != got {
		t.Fatalf("Get = %+v, want the stored %+v", read, got)
	}
}

func TestSetRetriesVersionConflicts(t *testing.T) {
	repo := &fakeRepo{conflicts: conflictAttempts - 1}
	got, err := newService(t, repo).SetProcessOnlyOnACPower(context.Background(), false)
	if err != nil {
		t.Fatalf("after %d conflicts: %v", conflictAttempts-1, err)
	}
	if got.ProcessOnlyOnACPower || repo.stored.ProcessOnlyOnACPower {
		t.Fatalf("toggle not stored: %+v, %+v", got, repo.stored)
	}
}

func TestSetGivesUpAfterConflicts(t *testing.T) {
	repo := &fakeRepo{conflicts: conflictAttempts}
	_, err := newService(t, repo).SetRecordingPromptsEnabled(context.Background(), false)
	if !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("err = %v, want ErrVersionConflict", err)
	}
	if repo.saves != 0 {
		t.Fatalf("saves = %d, want none", repo.saves)
	}
}

func TestLoadErrorIsReturned(t *testing.T) {
	boom := errors.New("disk")
	s := newService(t, &fakeRepo{loadErr: boom})
	if _, err := s.Get(context.Background()); !errors.Is(err, boom) {
		t.Errorf("Get: err = %v", err)
	}
	if _, err := s.SetProcessOnlyOnACPower(context.Background(), true); !errors.Is(err, boom) {
		t.Errorf("Set: err = %v", err)
	}
}
