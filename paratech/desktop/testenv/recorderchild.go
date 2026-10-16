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
	// RecorderFallback fails the process loopback activation and records in system mode (a switch with the cause
	// activation_failed).
	RecorderFallback = "fallback"
	// RecorderMute records process loopback and switches to system mode after RecorderMutePackets packets, as the
	// healthy-but-mute detector does (a switch with the cause healthy_but_mute and a new format).
	RecorderMute = "mute"
	// RecorderNoisy writes two bad lines to stdout first.
	RecorderNoisy = "noisy"
	// RecorderQuit stops by itself after 100 packets and reports "stopped", as if its stdin had closed.
	RecorderQuit = "quit"

	// The self-test ("Check sound") answers a self-test request with a FakeTone: by default both modes hear the tone.
	// RecorderFallback makes process loopback unsupported, RecorderUnavailable both, RecorderHang never answers.
	//
	// RecorderSilent: the captures never hear the tone.
	RecorderSilent = "silent"
	// RecorderNoDevice: there is no output device.
	RecorderNoDevice = "nodevice"
	// RecorderHandsFree: the output device is a Bluetooth hands-free endpoint.
	RecorderHandsFree = "handsfree"
)

// FakeSelfTestTiming is the short self-test of the fake recorder child and of the in-process child tests.
func FakeSelfTestTiming() audiocapture.SelfTestTiming {
	return audiocapture.SelfTestTiming{Tone: 300 * time.Millisecond, Lead: 50 * time.Millisecond, Tail: 100 * time.Millisecond,
		Open: 5 * time.Second}
}

// RecorderCrashPackets is when RecorderCrash exits: 40 s of audio, one closed chunk and one partial.
const RecorderCrashPackets = 400

// RecorderMutePackets is when RecorderMute switches: 5 s of audio.
const RecorderMutePackets = 50

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

	tone := &FakeTone{}
	src := &ToneSource{Format: FakeRecorderFormat(), PacketFrames: 800, Interval: 2 * time.Millisecond, Gate: tone.Audible}
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
		case RecorderMute:
			src.SwitchAfter, src.SwitchMode = RecorderMutePackets, audiocapture.ModeSystem
			src.SwitchFormat = audiocapture.Format{SampleRate: 16000, Channels: 1, BitsPerSample: 16}
		case RecorderSilent:
			src.Gate = func() bool { return false }
		case RecorderNoDevice:
			tone.NoDevice = true
		case RecorderHandsFree:
			tone.HandsFree = true
		case RecorderNoisy:
			fmt.Println("not an event")
			fmt.Println(strings.Repeat("x", 5000))
		default:
			fmt.Fprintln(os.Stderr, "fake recorder: unknown scenario", flag)
			return 1, true
		}
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := audiocapture.ChildConfig{StatsInterval: 50 * time.Millisecond, Tone: tone, SelfTest: FakeSelfTestTiming()}
	return audiocapture.RunChild(ctx, stdin, os.Stdout, src, store, log, cfg), true
}

// hangAfterStart reads the request, reports a start and then freezes.
func hangAfterStart() int {
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		return 1
	}
	fmt.Println(`{"type":"started","mode":"exclude","format":{"rate":8000,"channels":1,"bits":16},"windows":{"major":10,"minor":0,"build":19045}}`)
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
