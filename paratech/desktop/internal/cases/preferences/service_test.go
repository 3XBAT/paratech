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
		RecognitionLoad:         s.RecognitionLoad(),
		Profile:                 s.Profile().Fields(),
		FreeWorkNotice:          s.FreeWorkNotice(),
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
	want := Toggles{
		RecordingPromptsEnabled: settings.DefaultRecordingPromptsEnabled, ProcessOnlyOnACPower: settings.DefaultProcessOnlyOnACPower,
		RecognitionLoad: shared.DefaultRecognitionLoad, FreeWorkNotice: settings.DefaultFreeWorkNotice,
	}
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
	if got != (Toggles{RecognitionLoad: shared.LoadGentle, FreeWorkNotice: true}) {
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

// TASK-084: the load mode is stored like the toggles; an unknown mode is refused and changes nothing.
func TestSetRecognitionLoad(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{}
	s := newService(t, repo)

	got, err := s.SetRecognitionLoad(ctx, shared.LoadMax)
	if err != nil || got.RecognitionLoad != shared.LoadMax || repo.stored.RecognitionLoad != shared.LoadMax {
		t.Fatalf("SetRecognitionLoad(max) = %+v, %v; stored %q", got, err, repo.stored.RecognitionLoad)
	}
	if _, err := s.SetRecognitionLoad(ctx, "turbo"); !errors.Is(err, settings.ErrInvalidSettings) {
		t.Fatalf("unknown mode: err = %v, want ErrInvalidSettings", err)
	}
	if read, err := s.Get(ctx); err != nil || read.RecognitionLoad != shared.LoadMax {
		t.Fatalf("Get = %+v, %v; want max kept", read, err)
	}
}

// TASK-188: «Больше не показывать» turns the free-work card off, the toggle of «Настройки» on again; the other
// toggles stay.
func TestSetFreeWorkNotice(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{}
	s := newService(t, repo)

	got, err := s.SetFreeWorkNotice(ctx, false)
	if err != nil || got.FreeWorkNotice || repo.stored.FreeWorkNotice || !got.RecordingPromptsEnabled {
		t.Fatalf("off = %+v, %v; stored %+v", got, err, repo.stored)
	}
	if read, err := s.Get(ctx); err != nil || read.FreeWorkNotice {
		t.Fatalf("Get = %+v, %v; want off", read, err)
	}
	if got, err = s.SetFreeWorkNotice(ctx, true); err != nil || !got.FreeWorkNotice || !repo.stored.FreeWorkNotice {
		t.Fatalf("on = %+v, %v", got, err)
	}
}

// TASK-094: the student profile is stored trimmed with the other settings; a bad field changes nothing.
func TestSaveProfile(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{conflicts: 1}
	s := newService(t, repo)

	if got, err := s.Profile(ctx); err != nil || got != (settings.ProfileFields{}) {
		t.Fatalf("fresh profile = %+v, %v", got, err)
	}
	got, err := s.SaveProfile(ctx, settings.ProfileFields{FullName: " Иванов Иван ", Group: "ИВТ-21", City: "Москва"})
	want := settings.ProfileFields{FullName: "Иванов Иван", Group: "ИВТ-21", City: "Москва"}
	if err != nil || got != want || repo.stored.Profile != want {
		t.Fatalf("SaveProfile = %+v, %v; stored %+v", got, err, repo.stored.Profile)
	}
	if _, err := s.SaveProfile(ctx, settings.ProfileFields{Group: "a\nb"}); !errors.Is(err, settings.ErrInvalidSettings) {
		t.Fatalf("bad group: err = %v, want ErrInvalidSettings", err)
	}
	if read, err := s.Profile(ctx); err != nil || read != want {
		t.Fatalf("Profile = %+v, %v; want %+v kept", read, err, want)
	}
	// The toggles keep the profile and the profile keeps the toggles.
	if _, err := s.SetProcessOnlyOnACPower(ctx, false); err != nil || repo.stored.Profile != want {
		t.Fatalf("toggle dropped the profile: %v, %+v", err, repo.stored.Profile)
	}
	if _, err := s.SaveProfile(ctx, settings.ProfileFields{}); err != nil || repo.stored.ProcessOnlyOnACPower {
		t.Fatalf("clearing the profile: %v, AC only %v", err, repo.stored.ProcessOnlyOnACPower)
	}
}
