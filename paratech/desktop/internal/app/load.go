package app

import "runtime"

// logicalCPUs is the logical processor count the recognition load modes and the time estimates are computed for
// (TASK-084, ADR-0016); the engine and the estimate use the same number.
func logicalCPUs() int { return runtime.NumCPU() }
