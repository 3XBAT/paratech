package speedestimate

import (
	"math"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/loadpolicy"
	"paratech/desktop/internal/domain/transcription/values"
)

// Source tells where an estimate comes from.
type Source string

// Sources.
const (
	SourceUnknown  Source = "unknown"  // no table entry (a model outside the catalog) and nothing measured
	SourceTable    Source = "table"    // the rough table of ADR-0016: approximate
	SourceMeasured Source = "measured" // the moving average of runs on this computer in this mode
)

// Rules of the estimate (ADR-0016).
const (
	// HeavyPerHour: a model whose 1 h of audio takes longer gets the warning with the number.
	HeavyPerHour = time.Hour

	// MinRemaining is the least time left shown for a recognition that has not finished.
	MinRemaining = time.Minute

	// referenceThreads is the thread count the table factors are for.
	referenceThreads = 4

	// threadScaling: speed grows sublinearly with threads (memory bandwidth, hyper-threads).
	threadScaling = 0.7

	// quantizedSpeedup: a quantized model (q5, q8) takes about this share of the time of its f16 variant.
	quantizedSpeedup = 0.8

	// silenceSkip: with voice activity detection (always on, ADR-0030) a run takes about this share of the time
	// without it, since only the speech is recognised. It depends on the pauses: 0.5 on 5 min of a real lecture,
	// 0.94 on synthetic speech with pauses under 2 s (2026-10-26/27); 0.6 errs to the longer side until runs here
	// are measured.
	silenceSkip = 0.6
)

// tableFactor is the processing time over audio duration of a full (f16) model of the family with referenceThreads
// threads on a usual laptop CPU with AVX2, with the default decoder of whisper-cli (beam search) and without voice
// activity detection; 0 for an unknown family. Calibrated on small-q5_1 and base-q5_1 (RTF 0.80 and 0.28 on an
// i5-10300H, 2026-10-26); the other families keep their ratio to small. Rough numbers, see ADR-0016 and ADR-0030.
func tableFactor(f values.ModelFamily) float64 {
	switch f {
	case values.FamilyTiny:
		return 0.17
	case values.FamilyBase:
		return 0.35
	case values.FamilySmall:
		return 1.0
	case values.FamilyMedium:
		return 2.7
	case values.FamilyLargeV3Turbo:
		return 2.0
	case values.FamilyLargeV3:
		return 5.3
	}
	return 0
}

// Estimate is the processing time of 1 h of audio in one load mode.
type Estimate struct {
	load    shared.RecognitionLoad
	perHour time.Duration
	source  Source
}

// Load returns the mode the estimate is for.
func (e Estimate) Load() shared.RecognitionLoad { return e.load }

// PerHour returns the processing time of 1 h of audio; 0 when unknown.
func (e Estimate) PerHour() time.Duration { return e.perHour }

// Source returns where the estimate comes from.
func (e Estimate) Source() Source { return e.source }

// Known reports whether there is an estimate.
func (e Estimate) Known() bool { return e.source != SourceUnknown && e.perHour > 0 }

// Heavy reports whether 1 h of audio takes longer than HeavyPerHour.
func (e Estimate) Heavy() bool { return e.Known() && e.perHour > HeavyPerHour }

// For returns the processing time of audio; 0 when unknown.
func (e Estimate) For(audio time.Duration) time.Duration {
	if !e.Known() || audio <= 0 {
		return 0
	}
	return time.Duration(float64(e.perHour) * audio.Hours()).Round(time.Second)
}

// Estimator holds the rules. It has no state.
type Estimator struct {
	policy loadpolicy.Policy
}

// New returns the estimator.
func New() Estimator { return Estimator{policy: loadpolicy.New()} }

// Estimate returns the processing time of 1 h of audio for a model of the family and weight format in the mode on a
// computer with logicalCPUs logical processors. A measured speed of this model in this mode wins; otherwise the
// table factor is scaled by the silence skipped, the threads of the mode and, for a capped mode, by the share of the
// CPU it may use.
func (e Estimator) Estimate(family values.ModelFamily, q values.Quantization, load shared.RecognitionLoad,
	logicalCPUs int, measured values.MeasuredSpeed) Estimate {
	load = load.OrDefault()
	if !measured.IsZero() {
		return Estimate{load: load, perHour: perHour(measured.Factor()), source: SourceMeasured}
	}

	base := tableFactor(family) * silenceSkip
	if base == 0 {
		return Estimate{load: load, source: SourceUnknown}
	}
	if q.Reduced() {
		base *= quantizedSpeedup
	}

	p := e.policy.For(load, logicalCPUs)
	cores := float64(p.Threads())
	if c := p.CPUCapPercent(); c > 0 {
		cores = math.Min(cores, float64(max(logicalCPUs, 1))*float64(c)/100)
	}
	factor := base * math.Pow(referenceThreads/cores, threadScaling)
	return Estimate{load: load, perHour: perHour(factor), source: SourceTable}
}

// Remaining returns the time left of a recognition that takes total in all: all of it while queued; for a running
// one the share of total that its progress percent has not covered yet, or, before the first progress, total less
// the time since it started. The result is at least MinRemaining; 0 when total is unknown.
func (Estimator) Remaining(total time.Duration, running bool, percent int, sinceStart time.Duration) time.Duration {
	if total <= 0 {
		return 0
	}
	left := total
	switch {
	case !running:
	case percent > 0:
		left = time.Duration(float64(total) * float64(100-min(percent, 100)) / 100)
	default:
		left = total - max(sinceStart, 0)
	}
	return max(left, MinRemaining).Round(time.Second)
}

func perHour(factor float64) time.Duration {
	return time.Duration(factor * float64(time.Hour)).Round(time.Second)
}
