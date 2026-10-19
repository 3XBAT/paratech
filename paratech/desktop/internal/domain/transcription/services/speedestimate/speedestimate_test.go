package speedestimate

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

func TestEstimateTable(t *testing.T) {
	tests := []struct {
		name     string
		family   values.ModelFamily
		q        values.Quantization
		load     shared.RecognitionLoad
		cpus     int
		min, max time.Duration
		heavy    bool
	}{
		// 4 threads is the reference: the table factor itself.
		{"large normal", values.FamilyLargeV3, "f16", shared.LoadNormal, 8, 96 * time.Minute, 96 * time.Minute, true},
		{"medium q5 normal", values.FamilyMedium, "q5_0", shared.LoadNormal, 8, 38*time.Minute + 24*time.Second, 38*time.Minute + 24*time.Second, false},
		// 6 CPUs: 3 threads capped at 2.4 cores.
		{"large gentle on 6", values.FamilyLargeV3, "", shared.LoadGentle, 6, 2*time.Hour + 10*time.Minute, 2*time.Hour + 25*time.Minute, true},
		{"medium gentle on 6 is heavy", values.FamilyMedium, "", shared.LoadGentle, 6, 65 * time.Minute, 75 * time.Minute, true},
		{"turbo gentle on 8", values.FamilyLargeV3Turbo, "", shared.LoadGentle, 8, 40 * time.Minute, 45 * time.Minute, false},
		{"tiny max on 16", values.FamilyTiny, "", shared.LoadMax, 16, time.Minute, 75 * time.Second, false},
		{"small gentle on 1", values.FamilySmall, "", shared.LoadGentle, 1, 85 * time.Minute, 95 * time.Minute, true},
		{"unknown mode is gentle", values.FamilyLargeV3, "", "turbo", 6, 2*time.Hour + 10*time.Minute, 2*time.Hour + 25*time.Minute, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New().Estimate(tt.family, tt.q, tt.load, tt.cpus, values.MeasuredSpeed{})
			if e.Source() != SourceTable || !e.Known() {
				t.Fatalf("source %q known %v, want the table", e.Source(), e.Known())
			}
			if e.PerHour() < tt.min || e.PerHour() > tt.max {
				t.Errorf("per hour = %v, want %v..%v", e.PerHour(), tt.min, tt.max)
			}
			if e.Heavy() != tt.heavy {
				t.Errorf("heavy = %v, want %v", e.Heavy(), tt.heavy)
			}
			if e.Load() != tt.load.OrDefault() {
				t.Errorf("load = %q", e.Load())
			}
		})
	}
}

func TestEstimateModesAreOrdered(t *testing.T) {
	families := []values.ModelFamily{
		values.FamilyTiny, values.FamilyBase, values.FamilySmall, values.FamilyMedium, values.FamilyLargeV3Turbo,
		values.FamilyLargeV3,
	}
	for _, cpus := range []int{2, 4, 6, 8, 16} {
		for _, f := range families {
			g := New().Estimate(f, "", shared.LoadGentle, cpus, values.MeasuredSpeed{}).PerHour()
			n := New().Estimate(f, "", shared.LoadNormal, cpus, values.MeasuredSpeed{}).PerHour()
			m := New().Estimate(f, "", shared.LoadMax, cpus, values.MeasuredSpeed{}).PerHour()
			if g < n || n < m {
				t.Errorf("%s on %d CPUs: gentle %v, normal %v, max %v; want gentle >= normal >= max", f, cpus, g, n, m)
			}
			q := New().Estimate(f, "q8_0", shared.LoadNormal, cpus, values.MeasuredSpeed{}).PerHour()
			if q >= n {
				t.Errorf("%s on %d CPUs: quantized %v not faster than %v", f, cpus, q, n)
			}
		}
	}
}

func TestEstimateMeasuredAndUnknown(t *testing.T) {
	measured, err := values.RestoreMeasuredSpeed(0.25, 2)
	if err != nil {
		t.Fatal(err)
	}
	e := New().Estimate(values.FamilyLargeV3, "", shared.LoadGentle, 8, measured)
	if e.Source() != SourceMeasured || e.PerHour() != 15*time.Minute || e.Heavy() {
		t.Errorf("measured: %q %v heavy %v, want measured 15m", e.Source(), e.PerHour(), e.Heavy())
	}
	if got := e.For(2 * time.Hour); got != 30*time.Minute {
		t.Errorf("For(2h) = %v, want 30m", got)
	}

	// A model outside the catalog has no family: only a measurement tells.
	u := New().Estimate(values.FamilyNone, "q5_0", shared.LoadGentle, 8, values.MeasuredSpeed{})
	if u.Source() != SourceUnknown || u.Known() || u.Heavy() || u.PerHour() != 0 || u.For(time.Hour) != 0 {
		t.Errorf("unknown: %+v", u)
	}
	if m := New().Estimate(values.FamilyNone, "", shared.LoadMax, 8, measured); m.Source() != SourceMeasured {
		t.Errorf("unknown family with a measurement: source %q", m.Source())
	}
}

func TestRemaining(t *testing.T) {
	tests := []struct {
		name       string
		total      time.Duration
		running    bool
		percent    int
		sinceStart time.Duration
		want       time.Duration
	}{
		{"queued", 40 * time.Minute, false, 0, time.Hour, 40 * time.Minute},
		{"running with progress", 40 * time.Minute, true, 25, time.Hour, 30 * time.Minute},
		{"running before progress", 40 * time.Minute, true, 0, 10 * time.Minute, 30 * time.Minute},
		{"overdue before progress", 40 * time.Minute, true, 0, time.Hour, MinRemaining},
		{"almost done", 40 * time.Minute, true, 99, 0, MinRemaining},
		{"over 100 percent", 40 * time.Minute, true, 150, 0, MinRemaining},
		{"clock went back", 40 * time.Minute, true, 0, -time.Hour, 40 * time.Minute},
		{"unknown total", 0, true, 50, 0, 0},
	}
	for _, tt := range tests {
		if got := New().Remaining(tt.total, tt.running, tt.percent, tt.sinceStart); got != tt.want {
			t.Errorf("%s: Remaining = %v, want %v", tt.name, got, tt.want)
		}
	}
}
