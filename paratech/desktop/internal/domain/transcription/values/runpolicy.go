package values

import "fmt"

// MaxRunThreads bounds the thread count of one recognition run.
const MaxRunThreads = 256

// RunPolicy is how one speech recognition process runs (TASK-084): how many threads it uses, whether it runs with a
// lowered priority and the hard cap of its CPU use across all processors in percent (0: no cap). The loadpolicy
// service derives it from the load mode.
type RunPolicy struct {
	threads       int
	lowPriority   bool
	cpuCapPercent int
}

// NewRunPolicy checks threads (1..MaxRunThreads) and the cap (0 or 1..100).
func NewRunPolicy(threads int, lowPriority bool, cpuCapPercent int) (RunPolicy, error) {
	if threads < 1 || threads > MaxRunThreads {
		return RunPolicy{}, fmt.Errorf("%w: %d threads", ErrInvalidRunPolicy, threads)
	}
	if cpuCapPercent < 0 || cpuCapPercent > 100 {
		return RunPolicy{}, fmt.Errorf("%w: cpu cap %d percent", ErrInvalidRunPolicy, cpuCapPercent)
	}
	return RunPolicy{threads: threads, lowPriority: lowPriority, cpuCapPercent: cpuCapPercent}, nil
}

// Threads returns the thread count of the run.
func (p RunPolicy) Threads() int { return p.threads }

// LowPriority reports whether the process runs below the normal priority, so the user's apps go first.
func (p RunPolicy) LowPriority() bool { return p.lowPriority }

// CPUCapPercent returns the hard cap of the CPU use across all processors in percent; 0 means no cap.
func (p RunPolicy) CPUCapPercent() int { return p.cpuCapPercent }
