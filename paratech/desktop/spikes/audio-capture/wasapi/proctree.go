package wasapi

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoProcess means no running process has the requested executable name.
var ErrNoProcess = errors.New("wasapi: no such process")

// Proc is one entry of a process snapshot.
type Proc struct {
	PID       uint32
	ParentPID uint32
	Exe       string // executable file name, for example "chrome.exe"
}

// TreeRoot picks the PID to use as the INCLUDE target for exe: a process with that executable name whose parent
// is not a process with the same name (the browser main process, not a renderer). With several roots (two
// independent instances) it takes the one with the most same-name descendants, then the lowest PID.
//
// Parent PIDs can be reused by unrelated processes; the snapshot is a heuristic for the spike. A product picker
// would start from top-level windows instead, as OBS does.
func TreeRoot(procs []Proc, exe string) (uint32, error) {
	byPID := map[uint32]Proc{}
	for _, p := range procs {
		if p.PID != 0 && strings.EqualFold(p.Exe, exe) {
			byPID[p.PID] = p
		}
	}
	if len(byPID) == 0 {
		return 0, fmt.Errorf("%w: %s", ErrNoProcess, exe)
	}

	// rootOf follows same-name parents; the bound stops on a cycle that PID reuse could create.
	rootOf := func(p Proc) uint32 {
		for range len(byPID) {
			parent, ok := byPID[p.ParentPID]
			if !ok || parent.PID == p.PID {
				return p.PID
			}
			p = parent
		}
		return p.PID
	}

	size := map[uint32]int{}
	for _, p := range byPID {
		size[rootOf(p)]++
	}

	var best uint32
	for pid, n := range size {
		if best == 0 || n > size[best] || (n == size[best] && pid < best) {
			best = pid
		}
	}
	return best, nil
}
