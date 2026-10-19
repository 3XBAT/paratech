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

	tests := []struct {
		name        string
		m           MeasuredSpeed
		wall, audio time.Duration
		ok          bool
	}{
		{"short audio", MeasuredSpeed{}, 2 * time.Minute, MinMeasuredAudio - time.Second, false},
		{"exactly the shortest audio", MeasuredSpeed{}, 2 * time.Minute, MinMeasuredAudio, true},
		{"no wall time", MeasuredSpeed{}, 0, time.Hour, false},
		{"too fast", MeasuredSpeed{}, time.Second, time.Hour, false},
		{"too slow", MeasuredSpeed{}, 51 * time.Hour, time.Hour, false},
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

// A run far off the average counts as outlierRatio times off it: 0.5 + 0.3 * (1.5 - 0.5) and 0.5 + 0.3 * (0.5/3 - 0.5).
func TestObserveClampsOutliers(t *testing.T) {
	settled, err := RestoreMeasuredSpeed(0.5, 10)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		wall time.Duration
		want float64
	}{
		{"far above", 10 * time.Hour, 0.8},
		{"far below", 5 * time.Minute, 0.4},
		{"within", time.Hour, 0.65},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := settled.Observe(tt.wall, time.Hour)
			if !ok || math.Abs(got.Factor()-tt.want) > 1e-9 || got.Samples() != 11 {
				t.Errorf("Observe = %+v ok %v, want factor %v over 11", got, ok, tt.want)
			}
		})
	}
}

// A bad first run (a sleep of the computer) must not lock the average: steady real runs bring it back.
func TestObserveConvergesAfterABadFirstRun(t *testing.T) {
	m, ok := MeasuredSpeed{}.Observe(5*time.Hour, time.Hour)
	if !ok || m.Factor() != 5 {
		t.Fatalf("first run: %+v ok %v", m, ok)
	}
	for i := 0; i < 20; i++ {
		m, _ = m.Observe(30*time.Minute, time.Hour)
	}
	if math.Abs(m.Factor()-0.5) > 0.05 {
		t.Errorf("factor after 20 steady runs = %v, want near 0.5", m.Factor())
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
