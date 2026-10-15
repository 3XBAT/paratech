//go:build windows && (amd64 || arm64)

package audiocapture_test

import (
	"context"
	"os"
	"testing"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/testenv"
)

// TestLiveApps lists the real open apps and finds each of them again. It logs only executable names and counts,
// never window titles. Manual only, like TestLiveCapture.
func TestLiveApps(t *testing.T) {
	if os.Getenv("LA_AUDIO_LIVE_TEST") != "1" {
		t.Skip("set LA_AUDIO_LIVE_TEST=1 to run against the real windows and audio sessions")
	}
	testenv.NoGoroutineLeaks(t)
	apps := audiocapture.PlatformApps(nil)
	ctx := context.Background()

	list, err := apps.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	playing, icons := 0, 0
	for _, a := range list {
		if a.Playing {
			playing++
		}
		if a.IconPNG != nil {
			icons++
		}
		id, err := apps.Find(ctx, a.App)
		if err != nil {
			t.Errorf("Find(%s) = %v", a.App, err)
			continue
		}
		if same, err := apps.Same(ctx, id); err != nil || !same {
			t.Errorf("Same(%s) = %v, %v", a.App, same, err)
		}
		t.Logf("%s: pid %d, playing %v, muted %v, icon %d bytes", a.App, a.PID, a.Playing, a.Muted, len(a.IconPNG))
	}
	t.Logf("%d apps, %d playing, %d with an icon", len(list), playing, icons)
}
