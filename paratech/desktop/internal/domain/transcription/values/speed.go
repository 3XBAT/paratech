package values

import (
	"fmt"
	"math"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Bounds of a measured recognition speed (TASK-084). The factor is the wall time of a run over the duration of its
// audio: 0.5 means a 1 h recording took 30 min.
const (
	// MinMeasuredAudio is the shortest audio whose run counts: the wall time includes loading the model, which
	// would inflate the factor of a shorter recording.
	MinMeasuredAudio = 5 * time.Minute

	// MinSpeedFactor and MaxSpeedFactor bound a sane factor; a run outside (a clock jump, a sleep of the computer)
	// is ignored.
	MinSpeedFactor = 0.005
	MaxSpeedFactor = 50.0

	// speedSmoothing is the weight of a new run in the moving average.
	speedSmoothing = 0.3

	// A run is clamped to outlierRatio times off the average before it is added: one long sleep of the computer
	// moves the average only a little, yet a bad first run is still corrected by the following real ones.
	outlierRatio = 3.0

	// maxSpeedSamples caps the stored count; the weight of a new run does not depend on it.
	maxSpeedSamples = 1_000_000
)

// SpeedKey names what a speed is measured for: an installed model in a load mode.
type SpeedKey struct {
	Model shared.SttModelID
	Load  shared.RecognitionLoad
}

// MeasuredSpeed is the recognition speed of one model in one load mode on this computer: the exponential moving
// average of the speed factor of its runs and how many runs it is based on. The zero value means not measured.
type MeasuredSpeed struct {
	factor  float64
	samples int
}

// RestoreMeasuredSpeed rebuilds a stored speed; the factor must be sane and the count positive.
func RestoreMeasuredSpeed(factor float64, samples int) (MeasuredSpeed, error) {
	if !saneFactor(factor) || samples < 1 || samples > maxSpeedSamples {
		return MeasuredSpeed{}, fmt.Errorf("%w: factor %v over %d runs", ErrInvalidSpeed, factor, samples)
	}
	return MeasuredSpeed{factor: factor, samples: samples}, nil
}

// IsZero reports whether nothing is measured.
func (m MeasuredSpeed) IsZero() bool { return m.samples == 0 }

// Factor returns the average wall time over audio duration; 0 when not measured.
func (m MeasuredSpeed) Factor() float64 { return m.factor }

// Samples returns how many runs the average is based on.
func (m MeasuredSpeed) Samples() int { return m.samples }

// Observe returns the speed with one more successful run that took wall for audio. ok is false, and m is returned
// unchanged, when the run does not count: audio shorter than MinMeasuredAudio or a factor out of range. A factor more
// than outlierRatio times off the average counts as that bound.
func (m MeasuredSpeed) Observe(wall, audio time.Duration) (next MeasuredSpeed, ok bool) {
	if audio < MinMeasuredAudio || wall <= 0 {
		return m, false
	}
	f := wall.Seconds() / audio.Seconds()
	if !saneFactor(f) {
		return m, false
	}
	if m.IsZero() {
		return MeasuredSpeed{factor: f, samples: 1}, true
	}
	f = min(max(f, m.factor/outlierRatio), m.factor*outlierRatio)
	return MeasuredSpeed{
		factor:  m.factor + speedSmoothing*(f-m.factor),
		samples: min(m.samples+1, maxSpeedSamples),
	}, true
}

func saneFactor(f float64) bool {
	return !math.IsNaN(f) && f >= MinSpeedFactor && f <= MaxSpeedFactor
}
