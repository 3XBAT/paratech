package loadpolicy

import (
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// Rules of the modes (ADR-0016).
const (
	// GentleCPUCapPercent is the hard cap of «Бережная» across all processors.
	GentleCPUCapPercent = 40

	// NormalMaxThreads mirrors the whisper-cli default min(4, hardware threads) of «Обычная».
	NormalMaxThreads = 4
)

// Policy holds the rule. It has no state.
type Policy struct{}

// New returns the policy.
func New() Policy { return Policy{} }

// For returns the run policy of the mode on a computer with logicalCPUs logical processors (below 1 counts as 1):
//   - gentle: half the logical processors (at least 1, at most the threads of normal, so gentle never loads more than
//     normal on a many-core computer), below normal priority, CPU capped at GentleCPUCapPercent;
//   - normal: min(NormalMaxThreads, logical processors), below normal priority, no cap;
//   - max: every logical processor, normal priority, no cap.
//
// An unknown mode is treated as the default.
func (Policy) For(load shared.RecognitionLoad, logicalCPUs int) values.RunPolicy {
	n := min(max(logicalCPUs, 1), values.MaxRunThreads)

	var p values.RunPolicy
	var err error
	switch load.OrDefault() {
	case shared.LoadMax:
		p, err = values.NewRunPolicy(n, false, 0)
	case shared.LoadNormal:
		p, err = values.NewRunPolicy(min(NormalMaxThreads, n), true, 0)
	default:
		p, err = values.NewRunPolicy(max(1, min(n/2, NormalMaxThreads)), true, GentleCPUCapPercent)
	}
	if err != nil {
		// Unreachable: the values above are within the bounds of NewRunPolicy.
		p, _ = values.NewRunPolicy(1, true, GentleCPUCapPercent)
	}
	return p
}
