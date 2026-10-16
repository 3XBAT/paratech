package values

// SoundCause is the answer of the "Check sound" self-test (TASK-058, AC-3) for one capture mode or as a whole.
type SoundCause string

// Causes of a sound check.
const (
	// SoundOK: the tone was heard.
	SoundOK SoundCause = "ok"

	// SoundNoDevice: there is no active output device to play the tone on.
	SoundNoDevice SoundCause = "no_device"

	// SoundSilence: the capture ran but did not hear the tone.
	SoundSilence SoundCause = "silence"

	// SoundUnsupported: the mode does not start on this Windows (process loopback activation failed).
	SoundUnsupported SoundCause = "unsupported"

	// SoundHandsFree: the default output is a Bluetooth headset in hands-free (call) mode, which explains the silence.
	SoundHandsFree SoundCause = "hands_free"

	// SoundBusy: a recording runs; the check was refused, its tone would disturb the lecture.
	SoundBusy SoundCause = "busy"

	// SoundFailed: anything else.
	SoundFailed SoundCause = "failed"
)

// Valid reports whether c is one of the causes.
func (c SoundCause) Valid() bool {
	switch c {
	case SoundOK, SoundNoDevice, SoundSilence, SoundUnsupported, SoundHandsFree, SoundBusy, SoundFailed:
		return true
	}
	return false
}

// ModeCheck is the result of the self-test for one capture mode.
type ModeCheck struct {
	Mode    CaptureMode // include (process loopback of the test's own process) or system (classic loopback)
	Cause   SoundCause  // ok, no_device, silence, unsupported or failed
	HResult string      // of a failed activation, for support; empty otherwise
}

// SoundCheck is the outcome of the tone self-test: our own tone played on the default output device and captured with
// process loopback INCLUDE of the test process and with classic loopback (ADR-0002 Consequences TASK-058). Our tone is
// not in the chosen app's process tree, so it proves that this Windows build can capture, not that the app is
// audible. It holds no audio.
type SoundCheck struct {
	Busy      bool   // a recording ran: nothing was played or captured
	Device    string // friendly name of the default output device; a hardware name
	HandsFree bool   // the default output looks like a Bluetooth hands-free endpoint (a hint)
	Modes     []ModeCheck
}

// Result returns the check of mode; a mode that was not tested is failed.
func (c SoundCheck) Result(mode CaptureMode) ModeCheck {
	for _, m := range c.Modes {
		if m.Mode == mode {
			return m
		}
	}
	return ModeCheck{Mode: mode, Cause: SoundFailed}
}

// Verdict is the answer for a user who records source. An app is recorded with process loopback INCLUDE, so its
// result decides; the whole computer starts with process loopback and ends in classic loopback (ADR-0002 Decision 3),
// so it works when either does. Silence on a hands-free endpoint is reported as hands_free.
func (c SoundCheck) Verdict(source Source) SoundCause {
	if c.Busy {
		return SoundBusy
	}
	v := c.Result(CaptureInclude).Cause
	if source.IsWholeComputer() && v != SoundOK {
		v = c.Result(CaptureSystem).Cause
	}
	if !v.Valid() || v == SoundBusy || v == SoundHandsFree {
		return SoundFailed
	}
	if v == SoundSilence && c.HandsFree {
		return SoundHandsFree
	}
	return v
}
