package soundcheck

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

type fakeChecker struct {
	check values.SoundCheck
	err   error
	calls int
	hold  chan struct{} // when set, CheckSound waits for it
}

func (f *fakeChecker) CheckSound(ctx context.Context) (values.SoundCheck, error) {
	f.calls++
	if f.hold != nil {
		select {
		case <-f.hold:
		case <-ctx.Done():
			return values.SoundCheck{}, ctx.Err()
		}
	}
	return f.check, f.err
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

func modes(include, system values.SoundCause) values.SoundCheck {
	return values.SoundCheck{Device: "Speakers", Modes: []values.ModeCheck{
		{Mode: values.CaptureInclude, Cause: include}, {Mode: values.CaptureSystem, Cause: system},
	}}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name      string
		checker   *fakeChecker
		settings  fakeSettings
		recording bool
		want      values.SoundCause
		calls     int
	}{
		{"app: process loopback ok", &fakeChecker{check: modes(values.SoundOK, values.SoundSilence)},
			fakeSettings{app: "chrome.exe"}, false, values.SoundOK, 1},
		{"app: process loopback unsupported", &fakeChecker{check: modes(values.SoundUnsupported, values.SoundOK)},
			fakeSettings{app: "chrome.exe"}, false, values.SoundUnsupported, 1},
		{"whole computer: classic loopback is the fallback", &fakeChecker{check: modes(values.SoundUnsupported, values.SoundOK)},
			fakeSettings{}, false, values.SoundOK, 1},
		{"settings unreadable: whole computer", &fakeChecker{check: modes(values.SoundUnsupported, values.SoundOK)},
			fakeSettings{err: errors.New("db locked")}, false, values.SoundOK, 1},
		{"a recording runs", &fakeChecker{check: modes(values.SoundOK, values.SoundOK)},
			fakeSettings{}, true, values.SoundBusy, 0},
		{"the recorder refuses: busy", &fakeChecker{check: values.SoundCheck{Busy: true}},
			fakeSettings{}, false, values.SoundBusy, 1},
		{"no capture in this build", &fakeChecker{err: fmt.Errorf("x: %w", recording.ErrCaptureUnavailable)},
			fakeSettings{}, false, values.SoundUnsupported, 1},
		{"the check fails", &fakeChecker{err: errors.New("the child died")},
			fakeSettings{}, false, values.SoundFailed, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(tt.checker, tt.settings, fakeRecordings(tt.recording), nil)
			if err != nil {
				t.Fatal(err)
			}
			r, err := s.Check(context.Background())
			if err != nil || r.Verdict != tt.want || tt.checker.calls != tt.calls {
				t.Fatalf("Check = %+v, %v; %d calls; want %s", r, err, tt.checker.calls, tt.want)
			}
		})
	}
}

func TestCheckOneAtATime(t *testing.T) {
	c := &fakeChecker{check: modes(values.SoundOK, values.SoundOK), hold: make(chan struct{})}
	s, err := New(c, fakeSettings{}, fakeRecordings(false), nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Check(context.Background())
		done <- err
	}()
	for !waitLocked(s) {
		time.Sleep(time.Millisecond)
	}
	if _, err := s.Check(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second check = %v, want ErrBusy", err)
	}
	close(c.hold)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// waitLocked reports whether a check holds the lock.
func waitLocked(s *Service) bool {
	if s.running.TryLock() {
		s.running.Unlock()
		return false
	}
	return true
}

func TestCheckCancelled(t *testing.T) {
	c := &fakeChecker{hold: make(chan struct{})}
	s, err := New(c, fakeSettings{}, fakeRecordings(false), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Check(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Check = %v, want the cancellation", err)
	}
}

func TestNewNeedsEveryPort(t *testing.T) {
	c, set, rec := &fakeChecker{}, fakeSettings{}, fakeRecordings(false)
	for _, tt := range []struct {
		name string
		new  func() (*Service, error)
	}{
		{"checker", func() (*Service, error) { return New(nil, set, rec, nil) }},
		{"settings", func() (*Service, error) { return New(c, nil, rec, nil) }},
		{"recordings", func() (*Service, error) { return New(c, set, nil, nil) }},
	} {
		if _, err := tt.new(); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("no %s: %v", tt.name, err)
		}
	}
}
