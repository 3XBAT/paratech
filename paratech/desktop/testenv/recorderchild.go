package testenv

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/adapters/filestore"
)

// EnvFakeRecorder selects the scenario of the fake recorder child (RunFakeRecorderChild).
const EnvFakeRecorder = "LA_FAKE_RECORDER"

// Scenarios of the fake recorder child; EnvFakeRecorder may combine them with commas ("fallback,crash"). Every one
// captures a ToneSource (1 kHz, 8 kHz mono 16-bit, 100 ms
// packets at about 50 times real time), so a 30 s chunk closes after about 0.6 s.
const (
	// RecorderTone records until stdin closes.
	RecorderTone = "tone"
	// RecorderCrash exits with code 9 after RecorderCrashPackets packets, leaving its open chunk as .partial.
	RecorderCrash = "crash"
	// RecorderFail reports the device lost after 100 packets ("failed", exit code 3).
	RecorderFail = "fail"
	// RecorderHang reports "started" and then freezes without any event until it is killed.
	RecorderHang = "hang"
	// RecorderDeaf ignores the closed stdin and records until it is killed.
	RecorderDeaf = "deaf"
	// RecorderSlowStart never completes the activation.
	RecorderSlowStart = "slowstart"
	// RecorderUnavailable has no capture in this build.
	RecorderUnavailable = "unavailable"
	// RecorderFallback fails process loopback and records in system mode.
	RecorderFallback = "fallback"
	// RecorderNoisy writes two bad lines to stdout first.
	RecorderNoisy = "noisy"
	// RecorderQuit stops by itself after 100 packets and reports "stopped", as if its stdin had closed.
	RecorderQuit = "quit"
)

// RecorderCrashPackets is when RecorderCrash exits: 40 s of audio, one closed chunk and one partial.
const RecorderCrashPackets = 400

// FakeRecorderFormat is the format of the fake recorder child.
func FakeRecorderFormat() audiocapture.Format {
	return audiocapture.Format{SampleRate: 8000, Channels: 1, BitsPerSample: 16}
}

// RunFakeRecorderChild lets a test binary double as the recorder child: call it first in TestMain and exit with its
// code when ok is true. It is recognised by the production command line (--recorder --data-dir <dir>) together with
// EnvFakeRecorder; the chunks go through a filestore on <dir>/artifacts, as in the app. A --recorder command line
// without a scenario (the production recorder launched by mistake) exits with 1 at once instead of running the tests.
func RunFakeRecorderChild(args []string, getenv func(string) string) (code int, ok bool) {
	if len(args) == 0 || args[0] != "--recorder" {
		return 0, false
	}
	scenario := getenv(EnvFakeRecorder)
	if scenario == "" || len(args) != 3 || args[1] != "--data-dir" {
		fmt.Fprintln(os.Stderr, "fake recorder: no scenario; the test binary is not a recorder child")
		return 1, true
	}
	store, err := filestore.New(filepath.Join(args[2], "artifacts"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake recorder:", err)
		return 1, true
	}
	defer func() { _ = store.Close() }()

	src := &ToneSource{Format: FakeRecorderFormat(), PacketFrames: 800, Interval: 2 * time.Millisecond}
	var stdin io.Reader = os.Stdin
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, flag := range strings.Split(scenario, ",") {
		switch flag {
		case RecorderQuit:
			src.OnPacket = func(n int) {
				if n == 100 {
					cancel()
				}
			}
		case RecorderTone:
		case RecorderCrash:
			src.OnPacket = func(n int) {
				if n == RecorderCrashPackets {
					os.Exit(9)
				}
			}
		case RecorderFail:
			src.FailAfter = 100
		case RecorderHang:
			return hangAfterStart(), true
		case RecorderDeaf:
			stdin = io.MultiReader(os.Stdin, blockingReader{})
		case RecorderSlowStart:
			src.OpenBlock = true
		case RecorderUnavailable:
			src.OpenErr = audiocapture.ErrUnavailable
		case RecorderFallback:
			src.FallbackTo = audiocapture.ModeSystem
		case RecorderNoisy:
			fmt.Println("not an event")
			fmt.Println(strings.Repeat("x", 5000))
		default:
			fmt.Fprintln(os.Stderr, "fake recorder: unknown scenario", flag)
			return 1, true
		}
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := audiocapture.ChildConfig{StatsInterval: 50 * time.Millisecond}
	return audiocapture.RunChild(ctx, stdin, os.Stdout, src, store, log, cfg), true
}

// hangAfterStart reads the request, reports a start and then freezes.
func hangAfterStart() int {
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		return 1
	}
	fmt.Println(`{"type":"started","mode":"exclude","format":{"rate":8000,"channels":1,"bits":16},"build":"10.0.19045"}`)
	for {
		time.Sleep(time.Hour) // a sleeping process, not a deadlock the runtime would end
	}
}

// blockingReader never returns.
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) {
	for {
		time.Sleep(time.Hour)
	}
}
