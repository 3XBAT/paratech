//go:build windows && (amd64 || arm64)

package audiocapture_test

import (
	"context"
	"os"
	"testing"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/testenv"
)

// TestLiveSelfTest plays the real 1 kHz tone (about 1.5 s, -12 dBFS) on the default output device and captures it
// with process loopback INCLUDE of this process and with classic loopback, as "Check sound" does. Manual only: it is
// audible. Expect both modes "ok" on Windows 10 19045 with speakers or wired headphones.
func TestLiveSelfTest(t *testing.T) {
	if os.Getenv("LA_AUDIO_LIVE_TEST") != "1" {
		t.Skip("set LA_AUDIO_LIVE_TEST=1 to play a tone on the real output device")
	}
	testenv.NoGoroutineLeaks(t)
	tone := audiocapture.PlatformTone()
	dev, err := tone.Output()
	if err != nil {
		t.Fatalf("Output = %v", err)
	}
	t.Logf("output device %q, hands-free %v", dev.Name, dev.HandsFree)

	run := startChild(t, context.Background(), audiocapture.PlatformSource(audiocapture.DefaultSourceConfig()), selfTestLine,
		audiocapture.ChildConfig{Tone: tone})
	if code := run.exit(t); code != audiocapture.ChildExitOK {
		t.Fatalf("exit code %d", code)
	}
	results := run.out.find(t, "selftest")
	t.Logf("results %v", results)
	if len(results) != 2 {
		t.Fatalf("results %v, want one per mode", results)
	}
	for _, ev := range results {
		if ev["cause"] != "ok" {
			t.Errorf("%s: %v, want ok", ev["mode"], ev["cause"])
		}
	}
}
