//go:build windows

package whispercli

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// spinFor is how long the CPU-busy child keeps every processor busy.
const spinFor = 3 * time.Second

func queryCPURate(t *testing.T, job windows.Handle) jobCPURateControl {
	t.Helper()
	var info jobCPURateControl
	if err := windows.QueryInformationJobObject(job, windows.JobObjectCpuRateControlInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil { //nolint:gosec // the API takes the struct address
		t.Fatalf("QueryInformationJobObject: %v", err)
	}
	return info
}

// jobCPUTime is the user and kernel time of every process that ran in the job.
func jobCPUTime(t *testing.T, job windows.Handle) time.Duration {
	t.Helper()
	// JOBOBJECT_BASIC_ACCOUNTING_INFORMATION of winnt.h: four LARGE_INTEGER times in 100 ns, then four DWORD counts.
	var info struct {
		TotalUserTime, TotalKernelTime, ThisPeriodUserTime, ThisPeriodKernelTime int64
		PageFaults, Processes, Active, Terminated                                uint32
	}
	if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil { //nolint:gosec // the API takes the struct address
		t.Fatalf("QueryInformationJobObject: %v", err)
	}
	return time.Duration(info.TotalUserTime+info.TotalKernelTime) * 100
}

func TestCPUCapStruct(t *testing.T) {
	if n := unsafe.Sizeof(jobCPURateControl{}); n != 8 {
		t.Errorf("JOBOBJECT_CPU_RATE_CONTROL_INFORMATION is %d bytes, want 8", n)
	}
}

func TestProcGroupWithoutCap(t *testing.T) {
	g, err := newProcGroup(0)
	if g == nil || err != nil {
		t.Fatalf("newProcGroup(0) = %v, %v", g, err)
	}
	defer g.close()
	if info := queryCPURate(t, g.job); info.ControlFlags != 0 {
		t.Errorf("cap set without one: %+v", info)
	}
}

func TestSetCPUCapRefusesOutOfRange(t *testing.T) {
	g, err := newProcGroup(0)
	if g == nil || err != nil {
		t.Fatalf("newProcGroup(0) = %v, %v", g, err)
	}
	defer g.close()
	for _, c := range []int{-1, 0, 101} {
		if err := setCPUCap(g.job, c); err == nil {
			t.Errorf("setCPUCap(%d) succeeded", c)
		}
	}
}

// TestProcGroupCPUCapLive runs a harmless CPU-busy child (this test binary spinning on every processor) in a job
// capped at 40 percent: the cap reads back, the child runs below the normal priority, its CPU time stays well under
// what every processor could give it, and closing the group ends it.
func TestProcGroupCPUCapLive(t *testing.T) {
	if testing.Short() {
		t.Skip("spins the CPU for a few seconds")
	}
	g, err := newProcGroup(40)
	if g == nil || err != nil {
		t.Fatalf("newProcGroup(40) = %v, %v", g, err)
	}
	closed := false
	defer func() {
		if !closed {
			g.close()
		}
	}()

	info := queryCPURate(t, g.job)
	if info.ControlFlags != jobCPURateControlEnable|jobCPURateControlHardCap || info.CPURate != 4000 {
		t.Fatalf("cap reads back as %+v, want flags 5 and rate 4000", info)
	}

	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin) //nolint:gosec // the test binary itself
	cmd.Env = []string{spinEnvKey + "=" + strconv.FormatInt(spinFor.Milliseconds(), 10), goraceEnv}
	setProcAttrs(cmd, true)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	defer func() {
		if !closed {
			_ = cmd.Process.Kill()
			<-waited
		}
	}()
	start := time.Now()
	if err := g.attach(cmd.Process.Pid); err != nil {
		t.Fatalf("attach: %v", err)
	}

	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid)) //nolint:gosec // a live pid
	if err != nil {
		t.Fatal(err)
	}
	prio, err := windows.GetPriorityClass(h)
	_ = windows.CloseHandle(h)
	if err != nil || prio != windows.BELOW_NORMAL_PRIORITY_CLASS {
		t.Errorf("priority class = 0x%x, %v; want BELOW_NORMAL", prio, err)
	}

	time.Sleep(spinFor)
	wall := time.Since(start)
	used := jobCPUTime(t, g.job)
	share := used.Seconds() / (wall.Seconds() * float64(runtime.NumCPU()))
	t.Logf("cpu %v over %v on %d processors: %.0f%% of the computer", used, wall, runtime.NumCPU(), share*100)
	if used <= 0 {
		t.Error("the child used no CPU time: it did not run in the job")
	}
	// Lenient against the 40 percent cap: a loaded test machine only lowers the share.
	if share > 0.7 {
		t.Errorf("the capped child used %.0f%% of the computer, want well under 70%%", share*100)
	}

	// Closing the last handle of the job kills what still runs in it (KILL_ON_JOB_CLOSE).
	g.close()
	closed = true
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-waited
		t.Fatal("the child kept running after the job was closed")
	}
}
