package recordingsession

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var (
	activationSwitch = values.CaptureSwitch{From: values.CaptureInclude, To: values.CaptureSystem,
		Cause: values.SwitchActivationFailed, Reason: "activation failed: 0x80070005"}
	muteSwitch = values.CaptureSwitch{From: values.CaptureInclude, To: values.CaptureSystem,
		Cause: values.SwitchHealthyButMute, Reason: "healthy but mute: only zeros for 15s while the app played"}
)

func (e *env) startApp(app shared.AppName) Info {
	e.t.Helper()
	src, err := values.SourceOf(app)
	if err != nil {
		e.t.Fatal(err)
	}
	info, err := e.svc.Start(testCtx(e.t), StartRequest{Source: src})
	if err != nil {
		e.t.Fatalf("Start: %v", err)
	}
	return info
}

func (e *env) switchMode(id shared.RecordingID, sw values.CaptureSwitch) {
	e.t.Helper()
	if err := e.rec.SwitchMode(id, sw); err != nil {
		e.t.Fatal(err)
	}
}

// AC2 (TASK-058): a capture of an app that falls back to classic loopback, after an activation failure or a
// healthy-but-mute stream, keeps recording; the session stores the fixed note as the problem on the next heartbeat,
// once, and the completed recording keeps it.
func TestFallbackIsNotedAndTheRecordingGoesOn(t *testing.T) {
	for _, sw := range []values.CaptureSwitch{activationSwitch, muteSwitch} {
		t.Run(string(sw.Cause), func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			e := newEnv(t, heartbeatConfig())
			info := e.startApp("zoom.exe")
			e.switchMode(info.ID, sw)

			e.beat()
			e.awaitHeartbeat(info.ID, t0.Add(beatEvery))
			r := e.stored(info.ID)
			if r.Status() != recording.StatusRecording || r.Problem() != sw.Note() || strings.Contains(r.Problem(), "0x8007") {
				t.Fatalf("stored %s %q, want RECORDING with the note", r.Status(), r.Problem())
			}
			if active, ok := e.svc.Active(); !ok || active.Problem != sw.Note() || !e.rec.Running(info.ID) {
				t.Fatalf("active %+v, %v; the capture must go on", active, ok)
			}

			e.beat()
			e.awaitHeartbeat(info.ID, t0.Add(2*beatEvery))
			e.feed(info.ID, "a")
			got, err := e.svc.Stop(testCtx(t), info.ID)
			if err != nil || got.Status != recording.StatusCompleted || got.Problem != sw.Note() {
				t.Fatalf("Stop = %+v, %v; want COMPLETED with the note", got, err)
			}
			requireHistory(t, e.repo.statuses(), recording.StatusStarting, recording.StatusRecording,
				recording.StatusRecording, recording.StatusRecording, recording.StatusStopping, recording.StatusFinalizing,
				recording.StatusCompleted)
		})
	}
}

// A capture that stays in its mode (the app played normally, or it was muted, so nothing was expected and nothing
// switched) leaves no problem.
func TestNoFallbackNoProblem(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, heartbeatConfig())
	info := e.startApp("zoom.exe")
	e.beat()
	e.awaitHeartbeat(info.ID, t0.Add(beatEvery))
	if d, ok := e.rec.Diagnostics(testCtx(t), info.ID); !ok || d.Mode != values.CaptureInclude {
		t.Fatalf("diagnostics %+v, %v", d, ok)
	}
	e.feed(info.ID, "a")
	got, err := e.svc.Stop(testCtx(t), info.ID)
	if err != nil || got.Status != recording.StatusCompleted || got.Problem != "" {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
}

// A fallback after the last heartbeat is noted at the stop, before the recording leaves RECORDING.
func TestFallbackBeforeTheStopIsNoted(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig()) // no heartbeat in the test
	info := e.startApp("zoom.exe")
	e.feed(info.ID, "a")
	e.switchMode(info.ID, muteSwitch)
	got, err := e.svc.Stop(testCtx(t), info.ID)
	if err != nil || got.Status != recording.StatusCompleted || got.Problem != muteSwitch.Note() {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
	if r := e.stored(info.ID); r.Problem() != muteSwitch.Note() {
		t.Fatalf("stored problem %q", r.Problem())
	}
}

// A capture that falls back and later ends by itself keeps both: the reason first, then the note.
func TestFallbackThenTheCaptureEnds(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.startApp("zoom.exe")
	e.feed(info.ID, "a")
	e.switchMode(info.ID, activationSwitch)
	e.rec.End(info.ID, errors.New("device lost"))

	r := e.awaitStatus(info.ID, recording.StatusCompleted)
	if want := problemCaptureEnded + ": device lost; " + activationSwitch.Note(); r.Problem() != want {
		t.Fatalf("problem %q, want %q", r.Problem(), want)
	}
}

// The hard cap notes a fallback that came after the last heartbeat too.
func TestFallbackBeforeTheCapIsNoted(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	cfg := DefaultConfig() // no heartbeat before the cap
	cfg.MaxDuration = time.Minute
	e := newEnv(t, cfg)
	info := e.startApp("zoom.exe")
	e.feed(info.ID, "a")
	e.switchMode(info.ID, muteSwitch)
	e.sleeps.awaitOnly(t, time.Minute)
	e.clock.Advance(time.Minute)
	if r := e.awaitStatus(info.ID, recording.StatusCompleted); r.Problem() != muteSwitch.Note() {
		t.Fatalf("problem %q", r.Problem())
	}
}
