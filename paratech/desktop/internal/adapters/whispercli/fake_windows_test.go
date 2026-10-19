//go:build windows

package whispercli

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fakeProcess tells how the fake runs: its priority class and the CPU rate control of its job object
// ("<flags>/<rate>"; "0/0" without a cap).
func fakeProcess() []string {
	prio := "unknown"
	switch c, err := windows.GetPriorityClass(windows.CurrentProcess()); {
	case err != nil:
	case c == windows.BELOW_NORMAL_PRIORITY_CLASS:
		prio = prioBelow
	case c == windows.NORMAL_PRIORITY_CLASS:
		prio = prioNormal
	default:
		prio = fmt.Sprintf("0x%x", c)
	}

	// A zero handle is the job of the calling process.
	var info jobCPURateControl
	rate := "none"
	if err := windows.QueryInformationJobObject(0, windows.JobObjectCpuRateControlInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err == nil { //nolint:gosec // the API takes the struct address
		rate = fmt.Sprintf("%d/%d", info.ControlFlags, info.CPURate)
	}
	return []string{prio, rate}
}
