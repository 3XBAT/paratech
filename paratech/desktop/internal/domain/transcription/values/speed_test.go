package values

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestObserveFirstAndAverage(t *testing.T) {
	var m MeasuredSpeed
	if !m.IsZero() {
		t.Fatal("zero value is measured")
	}

	m, ok := m.Observe(30*time.Minute, time.Hour)
	if !ok || m.Factor() != 0.5 || m.Samples() != 1 {
		t.Fatalf("first run: %+v ok %v, want factor 0.5 over 1", m, ok)
	}

	m, ok = m.Observe(time.Hour, time.Hour) // factor 1: 0.5 + 0.3 * 0.5
	if !ok || math.Abs(m.Factor()-0.65) > 1e-9 || m.Samples() != 2 {
		t.Fatalf("second run: %+v ok %v, want factor 0.65 over 2", m, ok)
	}
}

func TestObserveRejectsAbsurdRuns(t *testing.T) {
	settled, err := RestoreMeasuredSpeed(0.5, outlierAfter)
	if err != nil {
		t.Fatal(err)
	}
	young, err := RestoreMeasuredSpeed(0.5, outlierAfter-1)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		m           MeasuredSpeed
		wall, audio time.Duration
		ok          bool
	}{
		{"short audio", MeasuredSpeed{}, 10 * time.Second, 59 * time.Second, false},
		{"exactly a minute", MeasuredSpeed{}, 30 * time.Second, time.Minute, true},
		{"no wall time", MeasuredSpeed{}, 0, time.Hour, false},
		{"too fast", MeasuredSpeed{}, time.Second, time.Hour, false},
		{"too slow", MeasuredSpeed{}, 51 * time.Hour, time.Hour, false},
		{"far above a settled average", settled, 2 * time.Hour, time.Hour, false},
		{"far below a settled average", settled, 5 * time.Minute, time.Hour, false},
		{"within a settled average", settled, time.Hour, time.Hour, true},
		{"far off a young average counts", young, 2 * time.Hour, time.Hour, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.m.Observe(tt.wall, tt.audio)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v (%+v)", ok, tt.ok, got)
			}
			if !ok && got != tt.m {
				t.Errorf("a rejected run changed the speed: %+v, want %+v", got, tt.m)
			}
		})
	}
}

func TestObserveCapsSamples(t *testing.T) {
	m, err := RestoreMeasuredSpeed(0.5, maxSpeedSamples)
	if err != nil {
		t.Fatal(err)
	}
	if m, _ = m.Observe(30*time.Minute, time.Hour); m.Samples() != maxSpeedSamples {
		t.Errorf("samples = %d, want the cap %d", m.Samples(), maxSpeedSamples)
	}
}

func TestRestoreMeasuredSpeed(t *testing.T) {
	for _, tt := range []struct {
		factor  float64
		samples int
		ok      bool
	}{
		{0.5, 1, true},
		{MinSpeedFactor, 1, true},
		{MaxSpeedFactor, maxSpeedSamples, true},
		{0.5, 0, false},
		{0.5, maxSpeedSamples + 1, false},
		{0, 1, false},
		{MaxSpeedFactor * 2, 1, false},
		{math.NaN(), 1, false},
		{math.Inf(1), 1, false},
	} {
		m, err := RestoreMeasuredSpeed(tt.factor, tt.samples)
		if tt.ok != (err == nil) {
			t.Errorf("RestoreMeasuredSpeed(%v, %d) = %+v, %v", tt.factor, tt.samples, m, err)
		}
		if !tt.ok && !errors.Is(err, ErrInvalidSpeed) {
			t.Errorf("err = %v, want ErrInvalidSpeed", err)
		}
	}
}
