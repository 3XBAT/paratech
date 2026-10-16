package values

import (
	"strings"
	"testing"
)

func TestCaptureMode(t *testing.T) {
	tests := []struct {
		mode    CaptureMode
		valid   bool
		process bool
	}{
		{CaptureInclude, true, true},
		{CaptureExclude, true, true},
		{CaptureSystem, true, false},
		{"", false, false},
		{"mic", false, false},
	}
	for _, tt := range tests {
		if tt.mode.Valid() != tt.valid || tt.mode.IsProcessLoopback() != tt.process {
			t.Errorf("%q: valid %v process %v", tt.mode, tt.mode.Valid(), tt.mode.IsProcessLoopback())
		}
	}
	for _, c := range []SwitchCause{SwitchActivationFailed, SwitchHealthyButMute, SwitchStreamFailed} {
		if !c.Valid() {
			t.Errorf("%s is a cause", c)
		}
	}
	if SwitchCause("").Valid() || SwitchCause("boom").Valid() {
		t.Error("unknown causes are valid")
	}
}

func TestCaptureSwitchNote(t *testing.T) {
	tests := []struct {
		name string
		sw   CaptureSwitch
		want []string
	}{
		{"app, activation", CaptureSwitch{From: CaptureInclude, To: CaptureSystem, Cause: SwitchActivationFailed},
			[]string{"Захват выбранного приложения", "activation failed", "весь звук компьютера"}},
		{"app, mute", CaptureSwitch{From: CaptureInclude, To: CaptureSystem, Cause: SwitchHealthyButMute},
			[]string{"только тишину", "only silence", "весь звук компьютера"}},
		{"app, stream", CaptureSwitch{From: CaptureInclude, To: CaptureSystem, Cause: SwitchStreamFailed},
			[]string{"прервался", "stream failed"}},
		{"whole computer", CaptureSwitch{From: CaptureExclude, To: CaptureSystem, Cause: SwitchActivationFailed},
			[]string{"Основной захват звука компьютера", "устройства вывода"}},
		{"unknown cause", CaptureSwitch{From: CaptureInclude, To: CaptureSystem}, []string{"не работал"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.sw.Reason = "secret detail 0x88890004"
			note := tt.sw.Note()
			for _, w := range tt.want {
				if !strings.Contains(note, w) {
					t.Errorf("note %q has no %q", note, w)
				}
			}
			if strings.Contains(note, "secret") || len(note) > 300 {
				t.Errorf("note %q (%d bytes) leaks the reason or is too long", note, len(note))
			}
		})
	}
}

func TestDiagnosticsFallback(t *testing.T) {
	d := CaptureDiagnostics{}
	if _, ok := d.Fallback(); ok {
		t.Fatal("no switch, no fallback")
	}
	sw := CaptureSwitch{From: CaptureInclude, To: CaptureSystem, Cause: SwitchHealthyButMute}
	d.Switches = []CaptureSwitch{{From: CaptureSystem, To: CaptureSystem}, sw}
	if got, ok := d.Fallback(); !ok || got != sw {
		t.Fatalf("Fallback = %+v, %v", got, ok)
	}
	if (CaptureSwitch{From: CaptureInclude, To: CaptureExclude}).IsFallback() {
		t.Fatal("only a switch to system is a fallback")
	}
}

func TestWindowsVersion(t *testing.T) {
	tests := []struct {
		name  string
		v     WindowsVersion
		win11 bool
		str   string
	}{
		{"windows 10", WindowsVersion{Product: "Windows 10 Pro", DisplayVersion: "22H2", Major: 10, Build: 19045, UBR: 6456},
			false, "Windows 10 Pro 22H2 (10.0.19045.6456)"},
		{"windows 11 named 10 in the registry", WindowsVersion{Product: "Windows 10 Home", DisplayVersion: "23H2", Major: 10,
			Build: 22631, UBR: 4317}, true, "Windows 11 Home 23H2 (10.0.22631.4317)"},
		{"first windows 11 build", WindowsVersion{Product: "Windows 10 Pro", Major: 10, Build: 22000}, true,
			"Windows 11 Pro (10.0.22000)"},
		{"build only", WindowsVersion{Major: 10, Build: 19045}, false, "(10.0.19045)"},
		{"server 2025", WindowsVersion{Product: "Windows Server 2025 Datacenter", DisplayVersion: "24H2", Major: 10, Build: 26100},
			false, "Windows Server 2025 Datacenter 24H2 (10.0.26100)"},
		{"nothing", WindowsVersion{}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.v.IsWindows11() != tt.win11 || tt.v.String() != tt.str {
				t.Fatalf("win11 %v, %q; want %v, %q", tt.v.IsWindows11(), tt.v.String(), tt.win11, tt.str)
			}
		})
	}
	if !(WindowsVersion{}).IsZero() || (WindowsVersion{Build: 1}).IsZero() {
		t.Fatal("IsZero")
	}
}

func TestStreamFormat(t *testing.T) {
	if got := (StreamFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true}).String(); got != "48000 Hz, 2 ch, 32-bit float" {
		t.Fatalf("String = %q", got)
	}
	if got := (StreamFormat{SampleRate: 44100, Channels: 2, BitsPerSample: 16}).String(); got != "44100 Hz, 2 ch, 16-bit PCM" {
		t.Fatalf("String = %q", got)
	}
	if !(StreamFormat{}).IsZero() || (StreamFormat{}).String() != "" {
		t.Fatal("zero format")
	}
}
