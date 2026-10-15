//go:build windows

package wasapi

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Processes returns a Toolhelp32 snapshot of the running processes.
func Processes() ([]Proc, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("wasapi: CreateToolhelp32Snapshot: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var (
		procs []Proc
		e     windows.ProcessEntry32
	)
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		procs = append(procs, Proc{PID: e.ProcessID, ParentPID: e.ParentProcessID, Exe: windows.UTF16ToString(e.ExeFile[:])})
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, fmt.Errorf("wasapi: Process32Next: %w", err)
	}
	return procs, nil
}

// FindTreeRoot returns the process tree root of the running executable exe (see TreeRoot).
func FindTreeRoot(exe string) (uint32, error) {
	procs, err := Processes()
	if err != nil {
		return 0, err
	}
	return TreeRoot(procs, exe)
}
