package soundcheck

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// fakeChecker reports levels, then waits at hold (when set) until it is released or ctx ends, and answers end, err.
type fakeChecker struct {
	levels []values.SoundLevel
	end    values.SoundCause
	err    error
	hold   chan struct{}

	mu      sync.Mutex
	calls   int
	source  values.Source
	d       time.Duration
	ctxErr  error
	waiting chan struct{} // closed when a call waits at hold
	report  func(values.SoundLevel)
}

func newHeld(levels ...values.SoundLevel) *fakeChecker {
	return &fakeChecker{levels: levels, end: values.SoundOK, hold: make(chan struct{}), waiting: make(chan struct{})}
}

func (f *fakeChecker) CheckLevels(ctx context.Context, source values.Source, d time.Duration,
	report func(values.SoundLevel)) (values.SoundCause, error) {
	f.mu.Lock()
	f.calls++
	f.source, f.d, f.report = source, d, report
	f.mu.Unlock()
	for _, l := range f.levels {
		report(l)
	}
	if f.hold != nil {
		close(f.waiting)
		select {
		case <-f.hold:
		case <-ctx.Done():
			f.mu.Lock()
			f.ctxErr = ctx.Err()
			f.mu.Unlock()
			return "", ctx.Err()
		}
	}
	return f.end, f.err
}

// hear reports a level from the running call, as the child does while the call waits at hold.
func (f *fakeChecker) hear(l values.SoundLevel) {
	f.mu.Lock()
	report := f.report
	f.mu.Unlock()
	report(l)
}

func (f *fakeChecker) seen() (calls int, source values.Source, d time.Duration, ctxErr error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.source, f.d, f.ctxErr
}

type fakeSettings struct {
	app shared.AppName
	err error
}

func (f fakeSettings) Load(context.Context) (*settings.Settings, error) {
	if f.err != nil {
		return nil, f.err
	}
	s := settings.New()
	if err := s.SetRecordingSource(f.app, time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)); err != nil {
		return nil, err
	}
	return s, nil
}

type fakeRecordings bool

func (f fakeRecordings) Recording() bool { return bool(f) }

var start = time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

func newService(t *testing.T, c LevelChecker, set fakeSettings, recording bool) (*Service, *testenv.FakeClock) {
	t.Helper()
	clock := testenv.NewFakeClock(start)
	s, err := New(Deps{Checker: c, Settings: set, Recordings: fakeRecordings(recording), Clock: clock}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, clock
}

// waitDone polls the status until the check has ended.
func waitDone(t *testing.T, s *Service) Status {
	t.Helper()
	testenv.Eventually(t, "the check ends", func() bool { return s.Status().State == StateDone }, nil)
	return s.Status()
}

func app(t *testing.T, name shared.AppName) values.Source {
	t.Helper()
	s, err := values.SourceOf(name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCheckVerdicts(t *testing.T) {
	loud := []values.SoundLevel{0, 0.3, 0.1}
	quiet := []values.SoundLevel{0, 0.004, 0}
	tests := []struct {
		name      string
		checker   *fakeChecker
		settings  fakeSettings
		recording bool
		want      values.SoundCause
		source    values.Source
		calls     int
	}{
		{"heard", &fakeChecker{levels: loud, end: values.SoundOK}, fakeSettings{}, false, values.SoundOK,
			values.WholeComputer(), 1},
		{"silence", &fakeChecker{levels: quiet, end: values.SoundOK}, fakeSettings{}, false, values.SoundSilence,
			values.WholeComputer(), 1},
		{"nothing reported", &fakeChecker{end: values.SoundOK}, fakeSettings{}, false, values.SoundSilence,
			values.WholeComputer(), 1},
		{"the remembered app", &fakeChecker{levels: loud, end: values.SoundOK}, fakeSettings{app: "zoom.exe"}, false,
			values.SoundOK, app(t, "zoom.exe"), 1},
		{"settings unreadable: whole computer", &fakeChecker{levels: loud, end: values.SoundOK},
			fakeSettings{err: errors.New("db locked")}, false, values.SoundOK, values.WholeComputer(), 1},
		{"the app does not run", &fakeChecker{err: fmt.Errorf("x: %w", recording.ErrSourceNotRunning)},
			fakeSettings{app: "zoom.exe"}, false, values.SoundNotRunning, app(t, "zoom.exe"), 1},
		{"no capture in this build", &fakeChecker{err: fmt.Errorf("x: %w", recording.ErrCaptureUnavailable)},
			fakeSettings{}, false, values.SoundUnsupported, values.WholeComputer(), 1},
		{"no output device", &fakeChecker{end: values.SoundNoDevice}, fakeSettings{}, false, values.SoundNoDevice,
			values.WholeComputer(), 1},
		{"the capture fails", &fakeChecker{levels: quiet, end: values.SoundFailed}, fakeSettings{}, false,
			values.SoundFailed, values.WholeComputer(), 1},
		{"heard before a failure", &fakeChecker{levels: loud, end: values.SoundFailed}, fakeSettings{}, false,
			values.SoundOK, values.WholeComputer(), 1},
		{"the checker breaks", &fakeChecker{err: errors.New("the child died")}, fakeSettings{}, false, values.SoundFailed,
			values.WholeComputer(), 1},
		{"a recording starts meanwhile", &fakeChecker{levels: loud, end: values.SoundBusy}, fakeSettings{}, false,
			values.SoundBusy, values.WholeComputer(), 1},
		{"a recording runs", &fakeChecker{levels: loud, end: values.SoundOK}, fakeSettings{}, true, values.SoundBusy,
			values.WholeComputer(), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			s, _ := newService(t, tt.checker, tt.settings, tt.recording)
			if _, err := s.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			st := waitDone(t, s)
			calls, source, d, _ := tt.checker.seen()
			if st.Verdict != tt.want || st.Source != tt.source || calls != tt.calls {
				t.Fatalf("status %+v, %d calls; want %s for %v", st, calls, tt.want, tt.source)
			}
			if calls > 0 && (source != tt.source || d != DefaultDuration) {
				t.Fatalf("checked %v for %v", source, d)
			}
			if st.Check.Level != 0 || st.Heard != (st.Check.Peak >= values.HeardLevel) {
				t.Fatalf("a done check %+v", st)
			}
		})
	}
}

// While it runs the status shows the loudest level since the previous Status, the loudest of the check, whether
// sound was heard and the time.
func TestStatusWhileRunning(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	c := newHeld(0.2, 0.05)
	s, clock := newService(t, c, fakeSettings{}, false)
	if st := s.Status(); st.State != StateIdle || st.Duration != DefaultDuration {
		t.Fatalf("before a check %+v", st)
	}
	st, err := s.Start(context.Background())
	if err != nil || st.State != StateRunning || st.Elapsed != 0 {
		t.Fatalf("Start = %+v, %v", st, err)
	}
	<-c.waiting
	clock.Advance(3 * time.Second)
	st = s.Status()
	if st.State != StateRunning || st.Check.Level != 0.2 || st.Check.Peak != 0.2 || !st.Heard ||
		st.Elapsed != 3*time.Second || st.Verdict != "" {
		t.Fatalf("running %+v", st)
	}
	if st := s.Status(); st.Check.Level != 0.2 {
		t.Fatalf("no new window: level %v, want it kept", st.Check.Level)
	}
	c.hear(0.03)
	c.hear(0.01)
	if st := s.Status(); st.Check.Level != 0.03 || st.Check.Peak != 0.2 {
		t.Fatalf("after a read: %+v, want the loudest new window 0.03", st.Check)
	}
	clock.Advance(time.Minute)
	if st := s.Status(); st.Elapsed != DefaultDuration {
		t.Fatalf("elapsed %v past the duration", st.Elapsed)
	}

	// One at a time: a second Start joins the running check.
	st, err = s.Start(context.Background())
	if !errors.Is(err, ErrBusy) || st.State != StateRunning {
		t.Fatalf("a second Start = %+v, %v", st, err)
	}
	close(c.hold)
	if st := waitDone(t, s); st.Verdict != values.SoundOK || st.Elapsed != DefaultDuration {
		t.Fatalf("done %+v", st)
	}
	if calls, _, _, _ := c.seen(); calls != 1 {
		t.Fatalf("%d calls", calls)
	}

	// A later Start checks again.
	c2 := &fakeChecker{end: values.SoundOK}
	s.deps.Checker = c2
	if _, err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := waitDone(t, s); st.Verdict != values.SoundSilence || st.Heard {
		t.Fatalf("the second check %+v", st)
	}
}

// blockingSettings holds Load until released.
type blockingSettings struct {
	entered chan struct{}
	release chan struct{}
}

func (b blockingSettings) Load(ctx context.Context) (*settings.Settings, error) {
	b.entered <- struct{}{}
	<-b.release
	return fakeSettings{}.Load(ctx)
}

// Start reads the settings without the lock: Status answers meanwhile, and of two concurrent Starts one runs the
// check and the other gets ErrBusy; a Close meanwhile wins.
func TestStartLoadsTheSourceWithoutTheLock(t *testing.T) {
	t.Run("concurrent starts", func(t *testing.T) {
		testenv.NoGoroutineLeaks(t)
		c := newHeld()
		set := blockingSettings{entered: make(chan struct{}, 2), release: make(chan struct{})}
		clock := testenv.NewFakeClock(start)
		s, err := New(Deps{Checker: c, Settings: set, Recordings: fakeRecordings(false), Clock: clock}, Config{})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()

		errs := make(chan error, 2)
		for range 2 {
			go func() {
				_, err := s.Start(context.Background())
				errs <- err
			}()
		}
		<-set.entered
		<-set.entered
		if st := s.Status(); st.State != StateIdle {
			t.Fatalf("status while the settings load %+v", st)
		}
		close(set.release)
		got := []error{<-errs, <-errs}
		oneRuns := got[0] == nil && errors.Is(got[1], ErrBusy) || got[1] == nil && errors.Is(got[0], ErrBusy)
		if !oneRuns {
			t.Fatalf("Start = %v, want one nil and one ErrBusy", got)
		}
		<-c.waiting
		close(c.hold)
		waitDone(t, s)
		if calls, _, _, _ := c.seen(); calls != 1 {
			t.Fatalf("%d checks started", calls)
		}
	})

	t.Run("close meanwhile", func(t *testing.T) {
		testenv.NoGoroutineLeaks(t)
		c := newHeld()
		set := blockingSettings{entered: make(chan struct{}, 1), release: make(chan struct{})}
		s, err := New(Deps{Checker: c, Settings: set, Recordings: fakeRecordings(false),
			Clock: testenv.NewFakeClock(start)}, Config{})
		if err != nil {
			t.Fatal(err)
		}
		errs := make(chan error, 1)
		go func() {
			_, err := s.Start(context.Background())
			errs <- err
		}()
		<-set.entered
		s.Close()
		close(set.release)
		if err := <-errs; !errors.Is(err, ErrClosed) {
			t.Fatalf("Start = %v, want ErrClosed", err)
		}
		if calls, _, _, _ := c.seen(); calls != 0 {
			t.Fatalf("%d checks started", calls)
		}
	})
}

// Stop ends the check early: ok when sound was heard, silence otherwise; the checker's ctx is cancelled.
func TestStop(t *testing.T) {
	tests := []struct {
		name   string
		levels []values.SoundLevel
		want   values.SoundCause
	}{
		{"heard", []values.SoundLevel{0.4}, values.SoundOK},
		{"nothing heard", []values.SoundLevel{0.001}, values.SoundSilence},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			c := newHeld(tt.levels...)
			s, clock := newService(t, c, fakeSettings{}, false)
			if _, err := s.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			<-c.waiting
			clock.Advance(4 * time.Second)
			st := s.Stop(context.Background())
			if st.State != StateDone || st.Verdict != tt.want || st.Elapsed != 4*time.Second {
				t.Fatalf("Stop = %+v", st)
			}
			if _, _, _, ctxErr := c.seen(); !errors.Is(ctxErr, context.Canceled) {
				t.Fatalf("the checker saw %v", ctxErr)
			}
		})
	}
}

func TestStopWithoutACheck(t *testing.T) {
	s, _ := newService(t, &fakeChecker{}, fakeSettings{}, false)
	if st := s.Stop(context.Background()); st.State != StateIdle {
		t.Fatalf("Stop = %+v", st)
	}
}

// The check outlives the request that started it.
func TestCheckIgnoresTheRequestContext(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	c := newHeld(0.3)
	s, _ := newService(t, c, fakeSettings{}, false)
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	<-c.waiting
	if st := s.Status(); st.State != StateRunning {
		t.Fatalf("status after the request ended %+v", st)
	}
	close(c.hold)
	if st := waitDone(t, s); st.Verdict != values.SoundOK {
		t.Fatalf("done %+v", st)
	}
}

// Close ends a running check and waits for it; then nothing starts.
func TestClose(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	c := newHeld()
	s, _ := newService(t, c, fakeSettings{}, false)
	if _, err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-c.waiting
	s.Close()
	if _, _, _, ctxErr := c.seen(); !errors.Is(ctxErr, context.Canceled) {
		t.Fatalf("the checker saw %v", ctxErr)
	}
	if st := s.Status(); st.State != StateDone {
		t.Fatalf("after Close %+v", st)
	}
	if _, err := s.Start(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Close = %v", err)
	}
	s.Close()
}

func TestNewValidates(t *testing.T) {
	c, set, rec, clock := &fakeChecker{}, fakeSettings{}, fakeRecordings(false), testenv.NewFakeClock(start)
	for _, tt := range []struct {
		name string
		deps Deps
		cfg  Config
	}{
		{"checker", Deps{Settings: set, Recordings: rec, Clock: clock}, Config{}},
		{"settings", Deps{Checker: c, Recordings: rec, Clock: clock}, Config{}},
		{"recordings", Deps{Checker: c, Settings: set, Clock: clock}, Config{}},
		{"clock", Deps{Checker: c, Settings: set, Recordings: rec}, Config{}},
		{"short", Deps{Checker: c, Settings: set, Recordings: rec, Clock: clock}, Config{Duration: time.Millisecond}},
		{"long", Deps{Checker: c, Settings: set, Recordings: rec, Clock: clock}, Config{Duration: time.Hour}},
	} {
		if _, err := New(tt.deps, tt.cfg); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
	s, err := New(Deps{Checker: c, Settings: set, Recordings: rec, Clock: clock}, Config{Duration: 5 * time.Second})
	if err != nil || s.Status().Duration != 5*time.Second {
		t.Fatalf("New = %v", err)
	}
	s.Close()
}
