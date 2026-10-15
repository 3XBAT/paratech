package recordingprompt

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var (
	t0        = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	lessonEnd = t0.Add(90 * time.Minute)
)

// fakeSettings is the settings store with the prompts switch the test sets.
type fakeSettings struct {
	mu    sync.Mutex
	off   bool // inverted: prompts are on by default
	err   error
	loads int
}

func (f *fakeSettings) Load(context.Context) (*settings.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	if f.err != nil {
		return nil, f.err
	}
	s := settings.New()
	if err := s.SetRecordingPromptsEnabled(!f.off, t0); err != nil {
		return nil, err
	}
	return s, nil
}

func (f *fakeSettings) setPrompts(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.off = !on
}

func (f *fakeSettings) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// fakeRecordings records the starts; hold makes the next starts wait until release.
type fakeRecordings struct {
	mu       sync.Mutex
	notReady bool
	err      error
	lessons  []shared.LessonID
	entered  chan struct{}
	release  chan struct{}
}

func (f *fakeRecordings) Ready() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.notReady
}

func (f *fakeRecordings) StartRecording(ctx context.Context, l shared.LessonID) (shared.RecordingID, error) {
	f.mu.Lock()
	f.lessons = append(f.lessons, l)
	entered, release, err := f.entered, f.release, f.err
	f.entered, f.release = nil, nil // one held start per hold
	f.mu.Unlock()
	if release != nil {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if err != nil {
		return "", err
	}
	return "rec-1", nil
}

// hold makes the next start wait until release; entered is closed when it waits.
func (f *fakeRecordings) hold() (entered <-chan struct{}, release func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entered, f.release = make(chan struct{}), make(chan struct{})
	rel := f.release
	var once sync.Once
	return f.entered, func() { once.Do(func() { close(rel) }) }
}

func (f *fakeRecordings) set(ready bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notReady, f.err = !ready, err
}

func (f *fakeRecordings) started() []shared.LessonID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]shared.LessonID(nil), f.lessons...)
}

type env struct {
	t          *testing.T
	clock      *testenv.FakeClock
	settings   *fakeSettings
	popup      *testenv.FakePopup
	recordings *fakeRecordings
	svc        *Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		t: t, clock: testenv.NewFakeClock(t0.Add(-5 * time.Minute)), settings: &fakeSettings{}, popup: &testenv.FakePopup{},
		recordings: &fakeRecordings{},
	}
	svc, err := New(Deps{
		Settings: e.settings, Popup: e.popup, Recordings: e.recordings, Clock: e.clock, Sleeper: e.clock,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

func announcement(id shared.LessonID) lesson.StartAnnounced {
	return lesson.StartAnnounced{
		LessonID: id, Title: "History <script>", Start: t0, End: lessonEnd, TimeZone: "Europe/Moscow",
		AnnouncedAt: t0.Add(-5 * time.Minute),
	}
}

// offer announces a lesson with prompts on and returns the current prompt.
func (e *env) offer(id shared.LessonID) View {
	e.t.Helper()
	if err := e.svc.LessonStarting(context.Background(), announcement(id)); err != nil {
		e.t.Fatal(err)
	}
	v, ok := e.svc.Current()
	if !ok {
		e.t.Fatal("no current prompt after the announcement")
	}
	return v
}

// at moves the clock to the given time.
func (e *env) at(when time.Time) { e.clock.Advance(when.Sub(e.clock.Now())) }
