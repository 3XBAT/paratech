package audiocapture_test

import (
	"context"
	"strings"
	"testing"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/testenv"
)

// The mode choice of a capture of an app (ADR-0002 Decision 3, TASK-058 AC2) through the real Recorder and a fake
// recorder child: an activation failure and a healthy-but-mute stream switch to classic loopback without stopping;
// an app that plays normally (or is muted, so nothing is expected) stays in INCLUDE. The start is logged with the
// Windows version, the mode, the app, the process, the device and the format (AC1).
func TestRecorderModeChoice(t *testing.T) {
	tests := []struct {
		name     string
		scenario string
		cause    values.SwitchCause // "" = no switch
		logs     []string
	}{
		{name: "activation failure", scenario: testenv.RecorderFallback, cause: values.SwitchActivationFailed,
			logs: []string{`msg="capture attempt failed"`, `hresult=0x80070005`, `cause=activation_failed`, `mode=system`}},
		{name: "healthy but mute", scenario: testenv.RecorderMute, cause: values.SwitchHealthyButMute,
			logs: []string{`cause=healthy_but_mute`, `reason="healthy but mute: only zeros for 15s"`, `mode=include`}},
		{name: "plays or not expected: stays include", scenario: testenv.RecorderTone, logs: []string{`mode=include`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			e, logs := newAppEnv(t, testConfig(), newFakeFinder(zoom1), tt.scenario)
			if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
				t.Fatal(err)
			}
			e.waitDiag(t, "the first chunk closes", func(d audiocapture.Diagnostics) bool { return d.Chunks >= 1 })

			d := e.diag(t)
			if d.TargetMode != audiocapture.ModeInclude || d.TargetPID != zoom1.PID || d.App != "zoom.exe" || !d.Running ||
				d.Windows != testenv.ToneWindows() || d.Device != testenv.ToneDevice {
				t.Fatalf("diagnostics = %+v", d)
			}
			sw, fell := d.Fallback()
			switch {
			case tt.cause == "" && (fell || d.Mode != audiocapture.ModeInclude):
				t.Fatalf("mode %s, switch %+v: the capture must stay include", d.Mode, sw)
			case tt.cause != "" && (!fell || sw.Cause != tt.cause || sw.From != audiocapture.ModeInclude || d.Mode != audiocapture.ModeSystem):
				t.Fatalf("mode %s, switches %+v; want a fallback for %s", d.Mode, d.Switches, tt.cause)
			}

			// The recording keeps running after a switch: one child, no restart, more audio.
			frames := d.Stats.Frames
			e.waitDiag(t, "the capture goes on", func(d audiocapture.Diagnostics) bool { return d.Stats.Frames > frames && d.Running })
			if err := stop(t, e); err != nil {
				t.Fatal(err)
			}
			if err := e.waitEnded(t); err != nil {
				t.Fatalf("Wait = %v", err)
			}
			if e.launcher.count() != 1 || e.diag(t).Restarts != 0 {
				t.Fatalf("%d children, %d restarts: a switch is no restart", e.launcher.count(), e.diag(t).Restarts)
			}

			out := logs.String()
			started := lineOf(out, `msg="capture started"`)
			for _, want := range []string{`windows="Windows 10 Pro 22H2 (10.0.19045.6456)"`, `build=10.0.19045.6456`,
				`target=include`, `app=zoom.exe`, `target_pid=300`, `device="Speakers (Fake Audio)"`, `format="8000 Hz, 1 ch, 16-bit PCM"`} {
				if !strings.Contains(started, want) {
					t.Errorf("the start line %q has no %s", started, want)
				}
			}
			for _, want := range tt.logs {
				if !strings.Contains(out, want) {
					t.Errorf("the log has no %s:\n%s", want, out)
				}
			}
		})
	}
}

// lineOf returns the first log line containing s.
func lineOf(log, s string) string {
	for line := range strings.SplitSeq(log, "\n") {
		if strings.Contains(line, s) {
			return line
		}
	}
	return ""
}
