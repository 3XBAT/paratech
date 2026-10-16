//go:build windows && (amd64 || arm64)

package audiocapture_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/testenv"
)

// liveObserver keeps the reports of the real source.
type liveObserver struct {
	mu       sync.Mutex
	attempts []audiocapture.Attempt
	switches []audiocapture.Switch
}

func (o *liveObserver) Attempt(a audiocapture.Attempt) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.attempts = append(o.attempts, a)
}

func (o *liveObserver) Switch(s audiocapture.Switch) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.switches = append(o.switches, s)
}

func (o *liveObserver) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return fmt.Sprintf("attempts %+v, switches %+v", o.attempts, o.switches)
}

// TestLiveCapture opens the real WASAPI streams (it plays nothing): process loopback EXCLUDE of this process and
// classic loopback, about 2 s each. Manual only: docs/tasks/TASK-017-recorder-manual-checks.md.
func TestLiveCapture(t *testing.T) {
	if os.Getenv("LA_AUDIO_LIVE_TEST") != "1" {
		t.Skip("set LA_AUDIO_LIVE_TEST=1 to run against the real audio devices")
	}
	testenv.NoGoroutineLeaks(t)
	src := audiocapture.PlatformSource(audiocapture.DefaultSourceConfig()) // one source: the process has one process loopback activation

	tests := []struct {
		target audiocapture.Target
		want   *audiocapture.Format // nil: the mix format of the output device
	}{
		{
			target: audiocapture.Target{Mode: audiocapture.ModeExclude, PID: uint32(os.Getpid())},
			want:   &audiocapture.Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true},
		},
		{target: audiocapture.Target{Mode: audiocapture.ModeSystem}},
	}
	for _, tt := range tests {
		t.Run(string(tt.target.Mode), func(t *testing.T) {
			obs := &liveObserver{}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			st, err := src.Open(ctx, tt.target, obs)
			if err != nil {
				t.Fatalf("Open = %v; %s", err, obs)
			}
			info := st.Info()
			t.Logf("info %+v, %s", info, obs)
			if info.Mode != tt.target.Mode || !strings.HasPrefix(info.Windows.BuildString(), "10.0.") {
				t.Errorf("Info = %+v, want mode %s on Windows 10/11", info, tt.target.Mode)
			}
			if err := info.Format.Validate(); err != nil || (tt.want != nil && info.Format != *tt.want) {
				t.Errorf("format %v (%v), want %v", info.Format, err, tt.want)
			}

			var packets, frames int
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				p, err := st.Read(ctx)
				if err != nil {
					t.Fatalf("Read after %d packets: %v", packets, err)
				}
				packets, frames = packets+1, frames+p.Frames
			}
			st.Stop()
			drained := 0
			for {
				_, err := st.Read(ctx)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("Read after Stop: %v", err)
				}
				drained++
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			t.Logf("%d packets, %d frames (%.2f s), %d drained after Stop, %d reopens", packets, frames,
				float64(frames)/float64(info.Format.SampleRate), drained, st.Reopens())
			if packets == 0 {
				t.Error("no packets in 2 s")
			}
		})
	}
}
