package values

import (
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/shared"
)

// CaptureMode is how a capture records audio (ADR-0002 Decisions 1 and 3).
type CaptureMode string

// Capture modes.
const (
	// CaptureInclude is process loopback of the process tree of the chosen app.
	CaptureInclude CaptureMode = "include"

	// CaptureExclude is the whole computer: process loopback of everything except this app's own process tree.
	CaptureExclude CaptureMode = "exclude"

	// CaptureSystem is classic loopback of the default output device: the one-way fallback of both other modes.
	CaptureSystem CaptureMode = "system"
)

// Valid reports whether m is one of the capture modes.
func (m CaptureMode) Valid() bool {
	return m == CaptureInclude || m == CaptureExclude || m == CaptureSystem
}

// IsProcessLoopback reports whether m is a process loopback mode (include or exclude).
func (m CaptureMode) IsProcessLoopback() bool { return m == CaptureInclude || m == CaptureExclude }

// SwitchCause is why a running capture fell back to classic loopback (ADR-0002 Decision 3).
type SwitchCause string

// Causes of a switch.
const (
	// SwitchActivationFailed: process loopback could not be activated or initialized (or timed out).
	SwitchActivationFailed SwitchCause = "activation_failed"

	// SwitchHealthyButMute: the process loopback stream delivered only silence for longer than the threshold while
	// the audio meters showed sound (the "healthy but mute" stream of MeetCap #38).
	SwitchHealthyButMute SwitchCause = "healthy_but_mute"

	// SwitchStreamFailed: the process loopback stream failed while running.
	SwitchStreamFailed SwitchCause = "stream_failed"
)

// Valid reports whether c is one of the causes.
func (c SwitchCause) Valid() bool {
	return c == SwitchActivationFailed || c == SwitchHealthyButMute || c == SwitchStreamFailed
}

// CaptureAttempt is one try to open a capture mode. It holds no audio content.
type CaptureAttempt struct {
	Mode    CaptureMode
	OK      bool
	HResult string // hex, for example "0x88890004"; empty when not a COM error
	Error   string // short text; on a successful try a non-fatal warning, if any
	Device  string // friendly name of the default output device at the try ("" when unknown); a hardware name
}

// CaptureSwitch is a mode change of a running capture: always to classic loopback, once per recording.
type CaptureSwitch struct {
	From, To CaptureMode
	Cause    SwitchCause
	Reason   string // technical detail for the log (error text, threshold); never shown as the problem
}

// IsFallback reports whether s is the fallback from process loopback to classic loopback.
func (s CaptureSwitch) IsFallback() bool { return s.From.IsProcessLoopback() && s.To == CaptureSystem }

// Note is the fixed text a recording keeps as its problem after the switch: what happened and what is recorded now.
// It names the cause in English too, for support. It never contains Reason (raw error texts stay in the log).
func (s CaptureSwitch) Note() string {
	what, why := "Захват выбранного приложения", "не работал"
	now := "записывается весь звук компьютера"
	if s.From == CaptureExclude {
		what, now = "Основной захват звука компьютера", "записывается звук устройства вывода по умолчанию"
	}
	switch s.Cause {
	case SwitchActivationFailed:
		why = "не запустился (activation failed)"
	case SwitchHealthyButMute:
		why = "давал только тишину, хотя звук играл (only silence)"
	case SwitchStreamFailed:
		why = "прервался (stream failed)"
	}
	return what + " " + why + " — " + now + "."
}

// StreamFormat is the format of a running capture stream: interleaved frames of signed PCM or IEEE float.
type StreamFormat struct {
	SampleRate    uint32
	Channels      uint16
	BitsPerSample uint16
	Float         bool
}

// IsZero reports whether no format is known.
func (f StreamFormat) IsZero() bool { return f == StreamFormat{} }

// String describes the format for logs: "48000 Hz, 2 ch, 32-bit float".
func (f StreamFormat) String() string {
	if f.IsZero() {
		return ""
	}
	kind := "PCM"
	if f.Float {
		kind = "float"
	}
	return fmt.Sprintf("%d Hz, %d ch, %d-bit %s", f.SampleRate, f.Channels, f.BitsPerSample, kind)
}

// windows11Build is the first build of Windows 11; its registry ProductName still says "Windows 10".
const windows11Build = 22000

// WindowsVersion identifies the Windows a capture runs on. Product and DisplayVersion are the registry texts
// ("Windows 10 Pro", "22H2"); the numbers come from RtlGetVersion and the registry UBR.
type WindowsVersion struct {
	Product        string
	DisplayVersion string
	Major, Minor   uint32
	Build, UBR     uint32
}

// IsZero reports whether nothing is known.
func (v WindowsVersion) IsZero() bool { return v == WindowsVersion{} }

// IsWindows11 reports a Windows 11 build (10.0.22000 and later) that is no Windows Server (Server 2025 has 26100).
func (v WindowsVersion) IsWindows11() bool {
	return v.Major == 10 && v.Build >= windows11Build && !strings.Contains(v.Product, "Server")
}

// Name is the product name with the Windows 11 correction: "Windows 11 Pro" for a build that the registry calls
// "Windows 10 Pro". Empty when the product is not known.
func (v WindowsVersion) Name() string {
	if v.IsWindows11() {
		if rest, ok := strings.CutPrefix(v.Product, "Windows 10"); ok {
			return "Windows 11" + rest
		}
	}
	return v.Product
}

// BuildString is "major.minor.build.ubr" ("10.0.19045.6456"), without ".ubr" when UBR is unknown; empty when no
// build is known.
func (v WindowsVersion) BuildString() string {
	if v.Major == 0 && v.Build == 0 {
		return ""
	}
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Build)
	if v.UBR != 0 {
		s += fmt.Sprintf(".%d", v.UBR)
	}
	return s
}

// String describes the version for logs: "Windows 11 Pro 23H2 (10.0.22631.4317)".
func (v WindowsVersion) String() string {
	parts := make([]string, 0, 3)
	for _, p := range []string{v.Name(), v.DisplayVersion} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if b := v.BuildString(); b != "" {
		parts = append(parts, "("+b+")")
	}
	return strings.Join(parts, " ")
}

// CaptureStats are the counters of a running capture. A restarted recorder child counts from zero again.
type CaptureStats struct {
	Packets       uint64
	SilentPackets uint64
	// AudiblePackets are the packets whose peak reaches HeardLevel (TASK-082): a stream of zeros that is not flagged
	// silent counts as no sound too.
	AudiblePackets uint64
	Frames         int64
	Reopens        uint64
}

// HeardSince reports whether the capture heard sound between the snapshots prev and s. Counters below prev mean a
// new recorder child, which counts from zero.
func (s CaptureStats) HeardSince(prev CaptureStats) bool {
	if s.Packets < prev.Packets || s.AudiblePackets < prev.AudiblePackets {
		return s.AudiblePackets > 0
	}
	return s.AudiblePackets > prev.AudiblePackets
}

// CaptureDiagnostics is a snapshot of the capture of one recording (TASK-058): what was asked for, what runs now, on
// which Windows and device, and what happened on the way. It holds no audio content and no window titles.
type CaptureDiagnostics struct {
	Running    bool
	TargetMode CaptureMode    // the mode asked for (include for an app, exclude for the whole computer)
	TargetPID  uint32         // the root process of the target tree; 0 for classic loopback
	App        shared.AppName // the chosen app; empty for the whole computer
	Mode       CaptureMode    // the mode the capture runs in now (after a fallback: system)
	Format     StreamFormat   // the format of the running stream
	Windows    WindowsVersion
	Device     string // friendly name of the output device of the running stream; a hardware name

	Attempts []CaptureAttempt // the latest tries, oldest first (bounded)
	Switches []CaptureSwitch  // the switches, oldest first (bounded)

	Chunks       int // closed chunks
	Stats        CaptureStats
	Restarts     int    // restarts of the recorder child
	Failure      string // why the capture or the last child ended unexpectedly
	InvalidLines int    // malformed or oversized event lines of the recorder child

	WaitingForApp bool // the app's process exited and no new instance is captured yet
	Rebinds       int  // new instances of the app captured
	// SourceSwitches counts the switches of the user to another source of the same recording (TASK-082). They are no
	// fallback and are not listed in Switches.
	SourceSwitches int
}

// Fallback returns the switch to classic loopback, if the capture made one (it is one-way, so there is at most one).
func (d CaptureDiagnostics) Fallback() (CaptureSwitch, bool) {
	for _, s := range d.Switches {
		if s.IsFallback() {
			return s, true
		}
	}
	return CaptureSwitch{}, false
}
