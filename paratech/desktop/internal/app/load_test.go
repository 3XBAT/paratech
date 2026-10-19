package app

import (
	"context"
	"testing"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/speedestimate"
)

// TASK-084: the wired core estimates every catalog model in every mode from the table, for the mode stored in the
// settings (gentle on a fresh installation).
func TestRecognitionEstimatesWired(t *testing.T) {
	ctx := context.Background()
	a, _ := newTestAppWith(t, t.TempDir())
	core := startCore(t, a)
	if core.Speed == nil {
		t.Fatal("no recognition time estimates in the core")
	}

	o, err := core.Speed.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if o.Load != shared.LoadGentle || len(o.Catalog) == 0 {
		t.Fatalf("fresh installation: mode %q, %d catalog estimates", o.Load, len(o.Catalog))
	}
	for id, e := range o.Catalog {
		if len(e.Modes) != 3 || e.Current.Source() != speedestimate.SourceTable || e.Current.PerHour() <= 0 {
			t.Errorf("%s: %+v, want three table estimates", id, e)
		}
	}

	if _, err := core.Settings.SetRecognitionLoad(ctx, shared.LoadMax); err != nil {
		t.Fatal(err)
	}
	if o, err := core.Speed.Overview(ctx); err != nil || o.Load != shared.LoadMax {
		t.Fatalf("after the change: %q, %v; want max", o.Load, err)
	}
}
