package audiocapture_test

import (
	"context"
	"testing"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/testenv"
)

// The support bundle lists the captures the recorder still knows: the ended ones first, then the running one.
func TestRecorderRecentCaptures(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newRecorderEnv(t, testConfig(), testenv.RecorderTone)
	ctx := context.Background()
	if got := e.rec.RecentCaptures(ctx); len(got) != 0 {
		t.Fatalf("RecentCaptures before any capture = %+v", got)
	}

	if err := e.rec.Start(ctx, "rec1", e.chunks, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	if err := e.rec.Start(ctx, "rec2", ref(t, "rec2"), values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.rec.Stop(context.Background(), "rec2") })

	got := e.rec.RecentCaptures(ctx)
	if len(got) != 2 || got[0].Running || !got[1].Running || got[1].Device != testenv.ToneDevice ||
		got[0].TargetMode != values.CaptureExclude {
		t.Fatalf("RecentCaptures = %+v", got)
	}
}
