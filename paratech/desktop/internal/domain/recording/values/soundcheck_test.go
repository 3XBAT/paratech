package values

import "testing"

func TestSoundCauseValid(t *testing.T) {
	for _, c := range []SoundCause{SoundOK, SoundNoDevice, SoundSilence, SoundUnsupported, SoundHandsFree, SoundBusy, SoundFailed} {
		if !c.Valid() {
			t.Errorf("%s is a cause", c)
		}
	}
	for _, c := range []SoundCause{"", "loud", "OK"} {
		if c.Valid() {
			t.Errorf("%q is not a cause", c)
		}
	}
}

func TestSoundCheckVerdict(t *testing.T) {
	app, err := SourceOf("chrome.exe")
	if err != nil {
		t.Fatal(err)
	}
	whole := WholeComputer()
	check := func(include, system SoundCause, handsFree bool) SoundCheck {
		return SoundCheck{HandsFree: handsFree, Modes: []ModeCheck{
			{Mode: CaptureInclude, Cause: include}, {Mode: CaptureSystem, Cause: system},
		}}
	}

	tests := []struct {
		name   string
		check  SoundCheck
		source Source
		want   SoundCause
	}{
		{"app: both ok", check(SoundOK, SoundOK, false), app, SoundOK},
		{"app: include decides", check(SoundUnsupported, SoundOK, false), app, SoundUnsupported},
		{"app: include ok, classic silent", check(SoundOK, SoundSilence, false), app, SoundOK},
		{"app: include silent", check(SoundSilence, SoundOK, false), app, SoundSilence},
		{"app: silence on a hands-free endpoint", check(SoundSilence, SoundSilence, true), app, SoundHandsFree},
		{"app: hands-free but heard", check(SoundOK, SoundSilence, true), app, SoundOK},
		{"whole: process loopback ok", check(SoundOK, SoundSilence, false), whole, SoundOK},
		{"whole: classic as the final fallback", check(SoundUnsupported, SoundOK, false), whole, SoundOK},
		{"whole: both fail, classic decides", check(SoundUnsupported, SoundSilence, false), whole, SoundSilence},
		{"whole: hands-free", check(SoundUnsupported, SoundSilence, true), whole, SoundHandsFree},
		{"no device", check(SoundNoDevice, SoundNoDevice, false), whole, SoundNoDevice},
		{"failed", check(SoundFailed, SoundFailed, true), app, SoundFailed},
		{"busy", SoundCheck{Busy: true}, app, SoundBusy},
		{"nothing tested", SoundCheck{}, whole, SoundFailed},
		{"a mode cannot be busy", check(SoundBusy, SoundOK, false), app, SoundFailed},
		{"an unknown cause", check("loud", SoundOK, false), app, SoundFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.check.Verdict(tt.source); got != tt.want {
				t.Fatalf("Verdict = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSoundCheckResult(t *testing.T) {
	c := SoundCheck{Modes: []ModeCheck{{Mode: CaptureInclude, Cause: SoundOK, HResult: ""}}}
	if got := c.Result(CaptureInclude); got.Cause != SoundOK {
		t.Fatalf("include = %+v", got)
	}
	if got := c.Result(CaptureSystem); got != (ModeCheck{Mode: CaptureSystem, Cause: SoundFailed}) {
		t.Fatalf("a mode not tested = %+v, want failed", got)
	}
}
