//go:build windows

package whispercli

import (
	"fmt"
	"time"
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

	waitForRunJob()

	// A zero handle is the job of the calling process.
	var info jobCPURateControl
	rate := "none"
	if err := windows.QueryInformationJobObject(0, windows.JobObjectCpuRateControlInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err == nil { //nolint:gosec // the API takes the struct address
		rate = fmt.Sprintf("%d/%d", info.ControlFlags, info.CPURate)
	}
	return []string{prio, rate}
}

// waitForRunJob waits, at most two seconds, until the parent has put this process into the job of the run (the
// limits newProcGroup sets), so the CPU rate is not read before the attach.
func waitForRunJob() {
	const runLimits = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_DIE_ON_UNHANDLED_EXCEPTION
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		err := windows.QueryInformationJobObject(0, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil) //nolint:gosec // the API takes the struct address
		if err == nil && info.BasicLimitInformation.LimitFlags&runLimits == runLimits {
			return
		}
	}
}
