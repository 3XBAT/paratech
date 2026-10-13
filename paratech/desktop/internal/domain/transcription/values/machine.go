package values

import "fmt"

// Machine is the hardware profile a model recommendation is made for (PRD section 8: recommend a model based on CPU
// and RAM; STT is CPU-only in MVP, so the GPU is not part of it). The zero value is an unknown machine.
type Machine struct {
	cpuCores int
	ramMB    int64
}

// NewMachine builds a known profile; both values must be positive. Use the zero Machine when detection failed.
func NewMachine(cpuCores int, ramMB int64) (Machine, error) {
	if cpuCores <= 0 || ramMB <= 0 {
		return Machine{}, fmt.Errorf("%w: %d cores, %d MB", ErrInvalidMachine, cpuCores, ramMB)
	}
	return Machine{cpuCores: cpuCores, ramMB: ramMB}, nil
}

// Known reports whether the profile was detected.
func (m Machine) Known() bool { return m.cpuCores > 0 }

// CPUCores returns the logical CPU count, 0 if unknown.
func (m Machine) CPUCores() int { return m.cpuCores }

// RAMMB returns the physical memory in MiB, 0 if unknown.
func (m Machine) RAMMB() int64 { return m.ramMB }
