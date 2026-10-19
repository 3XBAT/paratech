package whispercli

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// Priorities fakeProcess prints on Windows.
const (
	prioBelow  = "below"
	prioNormal = "normal"
)

// newLoadEngine is the fixture engine without a fixed thread count, for a computer with cpus logical processors.
func newLoadEngine(t *testing.T, f *fixture, cpus, threads int) *Engine {
	t.Helper()
	eng, err := New(Config{
		Binary: f.eng.cfg.Binary, DataRoot: f.root, WorkDir: f.work, Threads: threads, LogicalCPUs: cpus, Language: "ru",
	}, f.paths, f.store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	eng.env, eng.progressEvery = f.eng.env, 0
	return eng
}

// TASK-084: every load mode runs with its threads, priority and CPU cap (ADR-0016). The priority and the cap are
// checked from inside the process on Windows; elsewhere only the threads apply.
func TestTranscribeLoadModes(t *testing.T) {
	const (
		capped   = "5/4000" // JOB_OBJECT_CPU_RATE_CONTROL_ENABLE|HARD_CAP, 40.00 percent
		uncapped = "0/0"
	)
	tests := []struct {
		name          string
		cpus, threads int
		load          shared.RecognitionLoad
		wantThreads   string
		prio, rate    string
	}{
		{"gentle on 8", 8, 0, shared.LoadGentle, "4", prioBelow, capped},
		{"gentle on 6", 6, 0, shared.LoadGentle, "3", prioBelow, capped},
		{"normal on 8", 8, 0, shared.LoadNormal, "4", prioBelow, uncapped},
		{"normal on 2", 2, 0, shared.LoadNormal, "2", prioBelow, uncapped},
		{"max on 8", 8, 0, shared.LoadMax, "8", prioNormal, uncapped},
		{"max on 6", 6, 0, shared.LoadMax, "6", prioNormal, uncapped},
		{"unknown mode is gentle", 8, 0, "turbo", "4", prioBelow, capped},
		{"fixed threads keep the priority and the cap", 8, 2, shared.LoadGentle, "2", prioBelow, capped},
	}

	f := newFixture(t, "")
	model := f.model(t, modeProcess)
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := newLoadEngine(t, f, tt.cpus, tt.threads)
			out := outRef(t, "load-"+string(rune('a'+i))+".json")
			if err := eng.Transcribe(context.Background(), f.audio(t), model, out, tt.load, nil, nil); err != nil {
				t.Fatalf("Transcribe: %v", err)
			}

			var got []string
			for _, s := range f.stored(t, out).Segments {
				got = append(got, s.Text)
			}
			want := []string{tt.wantThreads}
			if runtime.GOOS == "windows" {
				want = append(want, tt.prio, tt.rate)
			}
			if !slices.Equal(got, want) {
				t.Errorf("the process saw %q, want %q", got, want)
			}
		})
	}
}

func TestRunPolicyDefaultsToTheCPUs(t *testing.T) {
	f := newFixture(t, "")
	eng := newLoadEngine(t, f, 0, 0)
	if eng.cpus != runtime.NumCPU() {
		t.Errorf("cpus = %d, want runtime.NumCPU() = %d", eng.cpus, runtime.NumCPU())
	}
	if p := eng.runPolicy(shared.LoadMax); p.Threads() != min(runtime.NumCPU(), maxThreads) {
		t.Errorf("max threads = %d, want every logical processor", p.Threads())
	}
}

type attachFunc func(pid int) error

func (f attachFunc) attach(pid int) error { return f(pid) }

// A process left outside the job runs without the CPU cap: that is logged like a cap that cannot be set, once.
func TestAttachFailureWarnsOfTheMissingCap(t *testing.T) {
	failing := attachFunc(func(int) error { return errors.New("access denied") })
	ok := attachFunc(func(int) error { return nil })
	tests := []struct {
		name   string
		grp    attacher
		cap    int
		capErr error
		warn   bool
	}{
		{"capped mode, attach fails", failing, 40, nil, true},
		{"capped mode, attached", ok, 40, nil, false},
		{"no cap in the mode", failing, 0, nil, false},
		{"cap error already logged", failing, 40, errors.New("create job object"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			e := &Engine{log: slog.New(slog.NewTextHandler(&buf, nil))}
			policy, err := values.NewRunPolicy(2, true, tt.cap)
			if err != nil {
				t.Fatal(err)
			}
			e.attach(tt.grp, 1234, policy, tt.capErr)
			if got := strings.Contains(buf.String(), "without the CPU cap"); got != tt.warn {
				t.Errorf("warned = %v, want %v; log %q", got, tt.warn, buf.String())
			}
		})
	}
}
