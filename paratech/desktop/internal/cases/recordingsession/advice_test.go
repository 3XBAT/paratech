package recordingsession

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

const adviceEvery = 5 * time.Second

// tickSleeper is the Sleeper of the advice tests: the advice loop sleeps until the test ticks; every other sleep runs
// on the fake clock. slept tells that the advice loop is idle, its last look done.
type tickSleeper struct {
	clock *testenv.FakeClock
	ticks chan struct{}
	slept chan struct{}
}

func (s *tickSleeper) Sleep(ctx context.Context, d time.Duration) error {
	if d != adviceEvery {
		return s.clock.Sleep(ctx, d)
	}
	select {
	case s.slept <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.ticks:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fakeApps is the open apps of a fake Windows.
type fakeApps struct {
	mu    sync.Mutex
	apps  []values.AudioApp
	err   error
	calls int
}

func (f *fakeApps) List(context.Context) ([]values.AudioApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return append([]values.AudioApp(nil), f.apps...), f.err
}

func (f *fakeApps) set(apps ...values.AudioApp) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps = apps
}

func (f *fakeApps) listed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// adviceLog records what the AdviceListener was told.
type adviceLog struct {
	mu   sync.Mutex
	told []values.SourceAdvice
}

func (l *adviceLog) SourceAdviceChanged(_ context.Context, _ shared.RecordingID, a values.SourceAdvice) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.told = append(l.told, a)
	return nil
}

func (l *adviceLog) all() []values.SourceAdvice {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]values.SourceAdvice(nil), l.told...)
}

type adviceEnv struct {
	*env
	sleeper *tickSleeper
	apps    *fakeApps
	told    *adviceLog
	idle    bool // the advice loop was seen idle since the last tick
}

func newAdviceEnv(t *testing.T) *adviceEnv {
	t.Helper()
	e := &env{
		t: t, clock: testenv.NewFakeClock(t0), repo: newMemRepo(), tr: newMemTranscripts(), tx: &fakeTx{},
		files: testenv.NewMemArtifacts(),
	}
	e.rec = testenv.NewFakeRecorder(e.files, chunkLen)
	e.hooks = &hookRecorder{FakeRecorder: e.rec}
	e.done = &completions{repo: e.repo}
	a := &adviceEnv{
		env:     e,
		sleeper: &tickSleeper{clock: e.clock, ticks: make(chan struct{}), slept: make(chan struct{})},
		apps:    &fakeApps{}, told: &adviceLog{},
	}
	cfg := DefaultConfig()
	cfg.HeartbeatInterval, cfg.AdviceInterval = heartbeatOff, adviceEvery
	svc, err := New(Deps{
		Recordings: e.repo, Transcripts: e.tr, Recorder: e.hooks, Artifacts: e.files, Tx: e.tx,
		Clock: e.clock, Sleeper: a.sleeper, IDs: &seqIDs{}, Completions: e.done, Apps: a.apps, Advice: a.told,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := svc.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return a
}

func (a *adviceEnv) startSource(app shared.AppName) Info {
	a.t.Helper()
	source, err := values.SourceOf(app)
	if err != nil {
		a.t.Fatal(err)
	}
	info, err := a.svc.Start(testCtx(a.t), StartRequest{Source: source})
	if err != nil {
		a.t.Fatalf("Start: %v", err)
	}
	a.idle = false
	return info
}

// wait waits until the advice loop has finished its last look.
func (a *adviceEnv) wait() {
	a.t.Helper()
	if a.idle {
		return
	}
	select {
	case <-a.sleeper.slept:
		a.idle = true
	case <-testCtx(a.t).Done():
		a.t.Fatal("the advice loop did not come back")
	}
}

// tick moves the clock by one interval and lets the advice loop look once.
func (a *adviceEnv) tick() {
	a.t.Helper()
	a.wait()
	a.clock.Advance(adviceEvery)
	a.sleeper.ticks <- struct{}{}
	a.idle = false
	a.wait()
}

func (a *adviceEnv) advice() values.SourceAdvice {
	a.t.Helper()
	info, ok := a.svc.Active()
	if !ok {
		a.t.Fatal("no recording in progress")
	}
	return info.Advice
}

// ownerApps: Yandex Browser chosen and silent, the video plays in Comet.
func ownerApps() []values.AudioApp {
	return []values.AudioApp{
		{App: "browser.exe", Name: "Яндекс Браузер"},
		{App: "comet.exe", Name: "Comet", Sounding: true, Playing: true},
	}
}

func cometSource(t *testing.T) values.Source {
	t.Helper()
	s, err := values.SourceOf("comet.exe")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TASK-082, the owner case: the chosen app gives only silence while Comet plays: after 15 s the advice shows (in the
// session and through the listener), not before; the user switches and the advice goes.
func TestAdviceAfterSilenceAndSwitch(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	a := newAdviceEnv(t)
	a.apps.set(ownerApps()...)
	info := a.startSource("browser.exe")

	for range 2 {
		a.tick()
	}
	if adv := a.advice(); !adv.IsZero() || len(a.told.all()) != 0 {
		t.Fatalf("advice after 10 s: %+v, told %v", adv, a.told.all())
	}
	a.tick()
	want := values.SourceAdvice{Silent: "browser.exe", SilentName: "Яндекс Браузер", Suggest: cometSource(t), SuggestName: "Comet"}
	if adv := a.advice(); adv != want {
		t.Fatalf("advice after 15 s = %+v, want %+v", adv, want)
	}
	if told := a.told.all(); len(told) != 1 || told[0] != want {
		t.Fatalf("listener told %+v", told)
	}

	if err := a.svc.SwitchSource(testCtx(t), info.ID, cometSource(t)); err != nil {
		t.Fatalf("SwitchSource = %v", err)
	}
	if src, _ := a.rec.Source(info.ID); src != cometSource(t) {
		t.Fatalf("the recorder captures %v", src)
	}
	cur, _ := a.svc.Active()
	if cur.Source != cometSource(t) || !cur.Advice.IsZero() {
		t.Fatalf("after the switch: source %v, advice %+v", cur.Source, cur.Advice)
	}
	if told := a.told.all(); len(told) != 2 || !told[1].IsZero() {
		t.Fatalf("listener told %+v, want the advice withdrawn", told)
	}
	if !a.rec.Running(info.ID) {
		t.Fatal("the recording does not go on")
	}
}

// The capture hears sound: no advice and no listing of the apps once it reports; a whole-computer source is never
// advised.
func TestNoAdvice(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	t.Run("the chosen app is heard", func(t *testing.T) {
		a := newAdviceEnv(t)
		a.apps.set(ownerApps()...)
		info := a.startSource("browser.exe")
		for i := range 6 {
			if err := a.rec.SetStats(info.ID, values.CaptureStats{Packets: uint64(100 * (i + 1)), AudiblePackets: uint64(10 * (i + 1))}); err != nil {
				t.Fatal(err)
			}
			a.tick()
		}
		// Only the first look, before the recorder reported anything, lists the apps.
		if adv := a.advice(); !adv.IsZero() || a.apps.listed() != 1 {
			t.Fatalf("advice %+v, %d listings", adv, a.apps.listed())
		}
	})
	t.Run("whole computer", func(t *testing.T) {
		a := newAdviceEnv(t)
		a.apps.set(ownerApps()...)
		a.startSource("")
		for range 6 {
			a.tick()
		}
		if adv := a.advice(); !adv.IsZero() || a.apps.listed() != 0 {
			t.Fatalf("advice %+v, %d listings", adv, a.apps.listed())
		}
	})
	t.Run("the apps cannot be listed", func(t *testing.T) {
		a := newAdviceEnv(t)
		a.apps.err = errors.New("access denied")
		a.startSource("browser.exe")
		for range 6 {
			a.tick()
		}
		if adv := a.advice(); !adv.IsZero() {
			t.Fatalf("advice %+v", adv)
		}
	})
}

// «Оставить» hides the advice; it comes back when the suggestion changes.
func TestDismissAdvice(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	a := newAdviceEnv(t)
	a.apps.set(ownerApps()...)
	info := a.startSource("browser.exe")
	for range 3 {
		a.tick()
	}
	if a.advice().IsZero() {
		t.Fatal("no advice")
	}
	if err := a.svc.DismissAdvice(testCtx(t), info.ID); err != nil {
		t.Fatal(err)
	}
	a.tick()
	if adv := a.advice(); !adv.IsZero() {
		t.Fatalf("dismissed advice shown: %+v", adv)
	}

	apps := append(ownerApps(), values.AudioApp{App: "zoom.exe", Name: "Zoom", Sounding: true})
	a.apps.set(apps...)
	a.tick()
	if adv := a.advice(); !adv.Suggest.IsWholeComputer() {
		t.Fatalf("a new suggestion must show: %+v", adv)
	}
}

func TestSwitchSourceRefused(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	t.Run("the app does not run", func(t *testing.T) {
		a := newAdviceEnv(t)
		info := a.startSource("browser.exe")
		a.rec.FailSwitch(recording.ErrSourceNotRunning)
		err := a.svc.SwitchSource(testCtx(t), info.ID, cometSource(t))
		if !errors.Is(err, recording.ErrSourceNotRunning) {
			t.Fatalf("SwitchSource = %v, want ErrSourceNotRunning", err)
		}
		cur, _ := a.svc.Active()
		if cur.Source.App() != "browser.exe" || !a.rec.Running(info.ID) {
			t.Fatalf("source %v after a failed switch", cur.Source)
		}
	})
	t.Run("the capture refuses", func(t *testing.T) {
		a := newAdviceEnv(t)
		info := a.startSource("browser.exe")
		a.rec.FailSwitch(errors.New("switch under way"))
		if err := a.svc.SwitchSource(testCtx(t), info.ID, cometSource(t)); !errors.Is(err, ErrSwitchRefused) {
			t.Fatalf("SwitchSource = %v, want ErrSwitchRefused", err)
		}
	})
	t.Run("during the stop", func(t *testing.T) {
		a := newAdviceEnv(t)
		info := a.startSource("browser.exe")
		a.feed(info.ID, "a")
		entered, release := a.rec.HoldFinalize()
		stopped := make(chan error, 1)
		go func() {
			_, err := a.svc.Stop(testCtx(t), info.ID)
			stopped <- err
		}()
		<-entered
		if err := a.svc.SwitchSource(testCtx(t), info.ID, cometSource(t)); !errors.Is(err, ErrSwitchRefused) {
			t.Fatalf("SwitchSource during the stop = %v, want ErrSwitchRefused", err)
		}
		release()
		if err := <-stopped; err != nil {
			t.Fatal(err)
		}
		if err := a.svc.SwitchSource(testCtx(t), info.ID, cometSource(t)); !errors.Is(err, ErrNotRunning) {
			t.Fatalf("SwitchSource of a finished recording = %v, want ErrNotRunning", err)
		}
		if err := a.svc.DismissAdvice(testCtx(t), info.ID); !errors.Is(err, ErrNotRunning) {
			t.Fatalf("DismissAdvice of a finished recording = %v, want ErrNotRunning", err)
		}
	})
	t.Run("unknown recording", func(t *testing.T) {
		a := newAdviceEnv(t)
		if err := a.svc.SwitchSource(testCtx(t), "nope", cometSource(t)); !errors.Is(err, shared.ErrNotFound) {
			t.Fatalf("SwitchSource = %v, want ErrNotFound", err)
		}
	})
}

// The advice of a recording that ends is withdrawn.
func TestAdviceWithdrawnAtTheEnd(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	a := newAdviceEnv(t)
	a.apps.set(ownerApps()...)
	info := a.startSource("browser.exe")
	for range 3 {
		a.tick()
	}
	a.feed(info.ID, "a")
	if _, err := a.svc.Stop(testCtx(t), info.ID); err != nil {
		t.Fatal(err)
	}
	told := a.told.all()
	if len(told) != 2 || told[0].IsZero() || !told[1].IsZero() {
		t.Fatalf("listener told %+v, want the advice and then none", told)
	}
}

// The advice ends with the capture: once a stop is asked for or the capture ends by itself, the frozen stats would
// read as silence, so the advice is withdrawn before the recording is assembled and the loop looks no more.
func TestAdviceEndsWithTheCapture(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	tests := []struct {
		name string
		end  func(a *adviceEnv, id shared.RecordingID) <-chan error
	}{
		{"stop", func(a *adviceEnv, id shared.RecordingID) <-chan error {
			stopped := make(chan error, 1)
			go func() {
				_, err := a.svc.Stop(testCtx(a.t), id)
				stopped <- err
			}()
			return stopped
		}},
		{"capture ended", func(a *adviceEnv, id shared.RecordingID) <-chan error {
			a.rec.End(id, errors.New("device lost"))
			done := make(chan error, 1)
			done <- nil
			return done
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newAdviceEnv(t)
			a.apps.set(ownerApps()...)
			info := a.startSource("browser.exe")
			for range 3 {
				a.tick()
			}
			if a.advice().IsZero() {
				t.Fatal("no advice")
			}
			a.feed(info.ID, "a")
			listed := a.apps.listed()

			entered, release := a.rec.HoldFinalize()
			defer release()
			ended := tt.end(a, info.ID)
			<-entered

			if told := a.told.all(); len(told) != 2 || !told[1].IsZero() {
				t.Fatalf("listener told %+v before the assembly, want the advice withdrawn", told)
			}
			if adv := a.advice(); !adv.IsZero() {
				t.Fatalf("advice during the assembly: %+v", adv)
			}
			a.clock.Advance(adviceEvery)
			select {
			case a.sleeper.ticks <- struct{}{}:
				t.Fatal("the advice loop still runs during the assembly")
			default:
			}

			release()
			if err := <-ended; err != nil {
				t.Fatal(err)
			}
			if told := a.told.all(); len(told) != 2 || a.apps.listed() != listed {
				t.Fatalf("listener told %+v, %d listings after the end (want %d)", told, a.apps.listed(), listed)
			}
		})
	}
}

// An observation taken before a switch is not counted against the new source.
func TestObservationOfAnOldSourceDropped(t *testing.T) {
	browser, err := values.SourceOf("browser.exe")
	if err != nil {
		t.Fatal(err)
	}
	sess := &session{watch: values.NewSourceWatch(browser)}
	quiet := values.SourceObservation{At: t0, Listed: true, Apps: ownerApps()}

	_, gen := sess.watching()
	sess.rewatch(cometSource(t))
	sess.observe(gen, quiet)
	if sess.watch != values.NewSourceWatch(cometSource(t)) {
		t.Fatalf("a stale observation changed the watch: %+v", sess.watch)
	}

	src, gen := sess.watching()
	sess.observe(gen, quiet)
	if src != cometSource(t) || sess.watch == values.NewSourceWatch(cometSource(t)) {
		t.Fatalf("a current observation of %v was dropped", src)
	}
}

// A switch whose caller gave up may still be done by the recorder: the session then follows the recorder's source,
// and a later switch to the same source is not skipped on the session's word.
func TestCancelledSwitchResyncsTheSource(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			a := newAdviceEnv(t)
			info := a.startSource("browser.exe")
			a.hooks.switchesThenFails(cause)

			err := a.svc.SwitchSource(testCtx(t), info.ID, cometSource(t))
			if !errors.Is(err, cause) || errors.Is(err, ErrSwitchRefused) {
				t.Fatalf("SwitchSource = %v, want %v", err, cause)
			}
			if cur, _ := a.svc.Active(); cur.Source != cometSource(t) {
				t.Fatalf("session source %v, the recorder captures comet.exe", cur.Source)
			}

			browser, err := values.SourceOf("browser.exe")
			if err != nil {
				t.Fatal(err)
			}
			if err := a.svc.SwitchSource(testCtx(t), info.ID, browser); err != nil {
				t.Fatal(err)
			}
			if src, _ := a.rec.Source(info.ID); src != browser {
				t.Fatalf("the recorder captures %v", src)
			}
		})
	}
}

// The session's source has drifted from the recorder's: a switch to the session's source still reaches the recorder.
func TestSwitchToTheSessionSourceReachesTheRecorder(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	a := newAdviceEnv(t)
	info := a.startSource("browser.exe")
	if err := a.rec.Switch(testCtx(t), info.ID, cometSource(t)); err != nil { // behind the session's back
		t.Fatal(err)
	}
	browser, err := values.SourceOf("browser.exe")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.svc.SwitchSource(testCtx(t), info.ID, browser); err != nil {
		t.Fatal(err)
	}
	if src, _ := a.rec.Source(info.ID); src != browser {
		t.Fatalf("the recorder captures %v, want browser.exe", src)
	}
}
