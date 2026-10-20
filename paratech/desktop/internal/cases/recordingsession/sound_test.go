package recordingsession

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/testenv"
)

// TASK-099: the live sound is read only while a recording captures, as the recorder reads it, and stops with the
// stop.
func TestSoundOfTheRunningRecording(t *testing.T) {
	e := newEnv(t, DefaultConfig())
	e.rec.SoundClock(e.clock.Now)
	ctx := testCtx(t)
	if s, ok := e.svc.Sound(ctx); ok {
		t.Fatalf("Sound without a recording = %+v", s)
	}

	info := e.start()
	if err := e.rec.HearLevel(info.ID, 0.5, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	var got LiveSound
	testenv.Eventually(t, "the sound is read", func() bool {
		var ok bool
		got, ok = e.svc.Sound(ctx)
		return ok
	}, nil)
	if want := (LiveSound{ID: info.ID, Reading: values.SoundReading{Level: 0.5, Heard: true}}); got != want {
		t.Fatalf("Sound = %+v, want %+v", got, want)
	}

	// Nothing heard for two minutes: the stale level reads as silence and the calm hint shows.
	e.clock.Advance(2 * time.Minute)
	got, ok := e.svc.Sound(ctx)
	if want := (LiveSound{ID: info.ID, Reading: values.SoundReading{Quiet: 2 * time.Minute, Hint: true}}); !ok || got != want {
		t.Fatalf("Sound = %+v, %v, want %+v", got, ok, want)
	}

	if _, err := e.stopAdvancing(info.ID); err != nil {
		t.Fatal(err)
	}
	if s, ok := e.svc.Sound(ctx); ok {
		t.Fatalf("Sound after the stop = %+v", s)
	}
}

// The reading is the recorder's: a jump of the session's clock shows no false silence.
func TestSoundIgnoresTheSessionClock(t *testing.T) {
	e := newEnv(t, DefaultConfig())
	at := e.clock.Now()
	e.rec.SoundClock(func() time.Time { return at })
	ctx := testCtx(t)
	info := e.start()
	if err := e.rec.HearLevel(info.ID, 0.5, at); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(10 * time.Minute)
	got, ok := e.svc.Sound(ctx)
	if want := (LiveSound{ID: info.ID, Reading: values.SoundReading{Level: 0.5, Heard: true}}); !ok || got != want {
		t.Fatalf("Sound = %+v, %v, want %+v", got, ok, want)
	}
	if _, err := e.stopAdvancing(info.ID); err != nil {
		t.Fatal(err)
	}
}

// A stopping recording reports no sound any more, even while its capture is still being stopped.
func TestNoSoundWhileStopping(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.rec.SoundClock(e.clock.Now)
	ctx := testCtx(t)
	info := e.start()
	if err := e.rec.HearLevel(info.ID, 0.5, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, "the sound is read", func() bool { _, ok := e.svc.Sound(ctx); return ok }, nil)

	entered := e.hooks.hangStops()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_, _ = e.svc.Stop(context.WithoutCancel(ctx), info.ID)
	}()
	<-entered
	if s, ok := e.svc.Sound(ctx); ok {
		t.Fatalf("Sound while stopping = %+v", s)
	}

	// The hard cap and the bound of the hanging Stop sleep; the bound ends the stop.
	if err := e.clock.AwaitSleepers(ctx, 2); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(detachedTimeout)
	<-stopped
}
