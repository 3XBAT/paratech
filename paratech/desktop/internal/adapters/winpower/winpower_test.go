package winpower

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
)

// powerMonitor is a copy of the jobrunner port: adapters cannot import cases.
type powerMonitor interface {
	IsOnACPower(ctx context.Context) (bool, error)
	Subscribe(ctx context.Context) (<-chan bool, error)
}

var _ powerMonitor = (*Monitor)(nil)

const tick = 2 * time.Millisecond

// source is a switchable power reading that counts reads.
type source struct {
	mu    sync.Mutex
	ac    bool
	err   error
	reads int
}

// set changes the reading and returns the read count at that moment.
func (s *source) set(ac bool, err error) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ac, s.err = ac, err
	return s.reads
}

func (s *source) read() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	return s.ac, s.err
}

// waitPolled waits until the poller has fully handled the reading set at read count n: read n+1 saw it, and
// read n+2 starts only after that poll iteration (including its send) finished. Sleeps alone are not enough
// on a loaded runner, where the poller can be descheduled for longer than any fixed delay.
func waitPolled(t *testing.T, s *source, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		r := s.reads
		s.mu.Unlock()
		if r >= n+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("poller stopped polling (blocked sender?)")
		}
		time.Sleep(tick)
	}
}

func recv(t *testing.T, ch <-chan bool) bool {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("channel closed")
		}
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for a value")
	}
	return false
}

func TestOnAC(t *testing.T) {
	tests := []struct {
		name   string
		status byte
		want   bool
	}{
		{"offline is battery", 0, false},
		{"online is AC", 1, true},
		{"unknown is AC", 255, true},
		{"unexpected value is AC", 7, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := onAC(tt.status); got != tt.want {
				t.Fatalf("onAC(%d) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestIsOnACPower(t *testing.T) {
	errRead := errors.New("boom")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name    string
		ctx     context.Context
		ac      bool
		readErr error
		want    bool
		wantErr bool
	}{
		{"ac", context.Background(), true, nil, true, false},
		{"battery", context.Background(), false, nil, false, false},
		{"read error", context.Background(), true, errRead, false, true},
		{"cancelled ctx", cancelled, true, nil, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &source{ac: tt.ac, err: tt.readErr}
			got, err := newMonitor(s.read).IsOnACPower(tt.ctx)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("got %v, %v; want %v, err=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestSubscribeDeliversCurrentThenChanges(t *testing.T) {
	s := &source{ac: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := newMonitor(s.read, WithInterval(tick)).Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !recv(t, ch) {
		t.Fatal("first value must be the current state (true)")
	}
	s.set(false, nil)
	if recv(t, ch) {
		t.Fatal("want false after unplug")
	}
	s.set(true, nil)
	if !recv(t, ch) {
		t.Fatal("want true after plug")
	}
}

func TestSubscribeIgnoresReadErrorsAndRepeats(t *testing.T) {
	s := &source{ac: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := newMonitor(s.read, WithInterval(tick)).Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recv(t, ch)
	waitPolled(t, s, s.set(false, errors.New("transient"))) // the failed read must not look like a change
	select {
	case v := <-ch:
		t.Fatalf("unexpected event %v", v)
	default:
	}
	s.set(false, nil)
	if recv(t, ch) {
		t.Fatal("want false once the read works again")
	}
}

func TestSubscribeSlowReaderSeesLatestAndSenderDoesNotBlock(t *testing.T) {
	s := &source{ac: true}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := newMonitor(s.read, WithInterval(tick)).Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The initial value stays unread while the source flips; the poller must keep running and the
	// reader must end up with the newest state, not a stale one.
	waitPolled(t, s, s.set(false, nil))
	waitPolled(t, s, s.set(true, nil))
	waitPolled(t, s, s.set(false, nil))
	if got := recv(t, ch); got {
		t.Fatal("want the latest state (false)")
	}
	cancel()
}

func TestSubscribeCancelClosesChannelWithoutLeak(t *testing.T) {
	s := &source{ac: true}
	before := runtime.NumGoroutine()
	var cancels []context.CancelFunc
	var chans []<-chan bool
	for range 5 {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := newMonitor(s.read, WithInterval(tick)).Subscribe(ctx)
		if err != nil {
			t.Fatal(err)
		}
		cancels, chans = append(cancels, cancel), append(chans, ch)
	}
	for _, c := range cancels {
		c()
	}
	for i, ch := range chans {
		deadline := time.After(2 * time.Second)
	drain:
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					break drain
				}
			case <-deadline:
				t.Fatalf("channel %d not closed after cancel", i)
			}
		}
	}
	for range 200 { // goroutines exit right after close
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: %d before, %d after", before, runtime.NumGoroutine())
}

func TestSubscribeErrors(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newMonitor((&source{}).read).Subscribe(cancelled); err == nil {
		t.Fatal("want error for cancelled ctx")
	}
	s := &source{err: errors.New("boom")}
	if _, err := newMonitor(s.read).Subscribe(context.Background()); err == nil {
		t.Fatal("want error when the initial read fails")
	}
}

func TestWithInterval(t *testing.T) {
	if got := newMonitor(nil).interval; got != DefaultInterval {
		t.Fatalf("default = %v", got)
	}
	if got := newMonitor(nil, WithInterval(0), WithInterval(-1)).interval; got != DefaultInterval {
		t.Fatalf("non-positive must be ignored, got %v", got)
	}
	if got := newMonitor(nil, WithInterval(time.Second)).interval; got != time.Second {
		t.Fatalf("got %v", got)
	}
}

func TestNewReadsOS(t *testing.T) {
	got, err := New().IsOnACPower(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && !got {
		t.Fatal("non-Windows stub must report AC")
	}
}
