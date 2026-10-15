package wasapi

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type fakeStream struct{ spec Spec }

func (f *fakeStream) Spec() Spec                 { return f.spec }
func (f *fakeStream) Format() Format             { return Format{} }
func (f *fakeStream) Stats() Stats               { return Stats{} }
func (f *fakeStream) Done() <-chan struct{}      { return nil }
func (f *fakeStream) Err() error                 { return nil }
func (f *fakeStream) Stop(context.Context) error { return nil }

type nopSink struct{}

func (nopSink) SetFormat(Format) error { return nil }
func (nopSink) Write([]byte) error     { return nil }

func TestOpenChain(t *testing.T) {
	include := Spec{Mode: ModeInclude, PID: 42}
	exclude := Spec{Mode: ModeExclude, PID: 7}
	system := Spec{Mode: ModeSystem}
	unsupported := hrError("ActivateAudioInterfaceAsync", hrNotImpl)

	tests := []struct {
		name      string
		specs     []Spec
		fail      map[Mode]error
		cancelled bool
		wantMode  Mode // 0: nothing starts
		wantTried []Mode
		wantHR    []HRESULT // HRESULT per attempt, 0 for none
	}{
		{name: "first mode starts", specs: []Spec{include, system}, wantMode: ModeInclude, wantTried: []Mode{ModeInclude}, wantHR: []HRESULT{0}},
		{
			name: "include fails, system starts", specs: []Spec{include, system},
			fail:     map[Mode]error{ModeInclude: unsupported},
			wantMode: ModeSystem, wantTried: []Mode{ModeInclude, ModeSystem}, wantHR: []HRESULT{hrNotImpl, 0},
		},
		{
			name: "everything fails", specs: []Spec{include, exclude, system},
			fail: map[Mode]error{
				ModeInclude: unsupported,
				ModeExclude: fmt.Errorf("wrapped: %w", hrError("IAudioClient::Initialize", hrUnsupportedFormat)),
				ModeSystem:  hrError("IMMDeviceEnumerator::GetDefaultAudioEndpoint", hrNotFound),
			},
			wantTried: []Mode{ModeInclude, ModeExclude, ModeSystem}, wantHR: []HRESULT{hrNotImpl, hrUnsupportedFormat, hrNotFound},
		},
		{name: "cancelled context tries nothing", specs: []Spec{include, system}, cancelled: true},
		{name: "no modes", specs: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}

			open := func(_ context.Context, s Spec, _ Sink) (Stream, error) {
				if err := tt.fail[s.Mode]; err != nil {
					return nil, err
				}
				return &fakeStream{spec: s}, nil
			}
			st, attempts, err := OpenChain(ctx, open, nopSink{}, tt.specs...)

			var tried []Mode
			var hrs []HRESULT
			for _, a := range attempts {
				tried = append(tried, a.Spec.Mode)
				hr, _ := HRESULTOf(a.Err)
				hrs = append(hrs, hr)
			}
			if !slices.Equal(tried, tt.wantTried) || !slices.Equal(hrs, tt.wantHR) {
				t.Fatalf("attempts %v with %v, want %v with %v", tried, hrs, tt.wantTried, tt.wantHR)
			}

			if tt.wantMode == 0 {
				if err == nil || st != nil {
					t.Fatalf("OpenChain = %v, %v; want an error", st, err)
				}
				for _, hr := range tt.wantHR {
					if !strings.Contains(err.Error(), fmt.Sprintf("0x%08X", uint32(hr))) {
						t.Errorf("error %q does not mention %s", err, hr)
					}
				}
				return
			}
			if err != nil || st.Spec().Mode != tt.wantMode {
				t.Fatalf("OpenChain = %v, %v; want mode %s", st, err, tt.wantMode)
			}
		})
	}
}

func TestAttemptString(t *testing.T) {
	tests := []struct {
		a    Attempt
		want string
	}{
		{Attempt{Spec: Spec{Mode: ModeSystem}}, "system: ok"},
		{Attempt{Spec: Spec{Mode: ModeInclude, PID: 9}, Err: hrError("op", hrDeviceInvalidated)}, "include(pid=9): hresult=0x88890004 (AUDCLNT_E_DEVICE_INVALIDATED) wasapi: op failed: 0x88890004 (AUDCLNT_E_DEVICE_INVALIDATED)"},
		{Attempt{Spec: Spec{Mode: ModeExclude, PID: 1}, Err: errors.New("boom")}, "exclude(pid=1): boom"},
	}
	for _, tt := range tests {
		if got := tt.a.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestSpecValidate(t *testing.T) {
	tests := []struct {
		s  Spec
		ok bool
	}{
		{Spec{Mode: ModeInclude, PID: 1}, true},
		{Spec{Mode: ModeExclude, PID: 1}, true},
		{Spec{Mode: ModeSystem}, true},
		{Spec{Mode: ModeInclude}, false},
		{Spec{Mode: ModeExclude}, false},
		{Spec{Mode: ModeSystem, PID: 3}, false},
		{Spec{Mode: 0}, false},
		{Spec{Mode: 99, PID: 1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.s.String(), func(t *testing.T) {
			err := tt.s.Validate()
			if (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalidSpec)) {
				t.Fatalf("Validate() = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestProcessLoopbackMode(t *testing.T) {
	tests := []struct {
		m    Mode
		want uint32
		ok   bool
	}{
		{ModeInclude, 0, true},
		{ModeExclude, 1, true},
		{ModeSystem, 0, false},
	}
	for _, tt := range tests {
		if got, ok := tt.m.processLoopbackMode(); got != tt.want || ok != tt.ok {
			t.Errorf("%s: got %d %v, want %d %v", tt.m, got, ok, tt.want, tt.ok)
		}
	}
}

func TestHRESULT(t *testing.T) {
	tests := []struct {
		hr     HRESULT
		failed bool
		text   string
	}{
		{hrOK, false, "0x00000000 (S_OK)"},
		{hrFalse, false, "0x00000001 (S_FALSE)"},
		{hrBufferEmpty, false, "0x08890001 (AUDCLNT_S_BUFFER_EMPTY)"},
		{hrNotImpl, true, "0x80004001 (E_NOTIMPL)"},
		{hrDeviceInvalidated, true, "0x88890004 (AUDCLNT_E_DEVICE_INVALIDATED)"},
		{0x8889FFFF, true, "0x8889FFFF"},
	}
	for _, tt := range tests {
		if tt.hr.Failed() != tt.failed || tt.hr.String() != tt.text {
			t.Errorf("%08X: Failed=%v String=%q, want %v %q", uint32(tt.hr), tt.hr.Failed(), tt.hr.String(), tt.failed, tt.text)
		}
	}

	wrapped := fmt.Errorf("outer: %w", hrError("IAudioCaptureClient::GetBuffer", hrDeviceInvalidated))
	var e *Error
	if !errors.As(wrapped, &e) || e.HR != hrDeviceInvalidated || e.Op != "IAudioCaptureClient::GetBuffer" {
		t.Fatalf("errors.As = %+v", e)
	}
	if !isDeviceLost(wrapped) || isDeviceLost(hrError("x", hrNotImpl)) || isDeviceLost(errors.New("x")) {
		t.Fatal("isDeviceLost misclassifies")
	}
	if _, ok := HRESULTOf(errors.New("plain")); ok {
		t.Fatal("HRESULTOf found a code in a plain error")
	}
}

// waveFormatEx builds a WAVEFORMATEX; ext adds the WAVEFORMATEXTENSIBLE tail with sub-format tag sub.
func waveFormatEx(tag, ch uint16, rate uint32, bits uint16, ext bool, sub uint32) []byte {
	le := binary.LittleEndian
	block := ch * bits / 8
	b := le.AppendUint16(nil, tag)
	b = le.AppendUint16(b, ch)
	b = le.AppendUint32(b, rate)
	b = le.AppendUint32(b, rate*uint32(block))
	b = le.AppendUint16(b, block)
	b = le.AppendUint16(b, bits)
	if !ext {
		return le.AppendUint16(b, 0)
	}
	b = le.AppendUint16(b, extensibleExtra)
	b = le.AppendUint16(b, bits) // wValidBitsPerSample
	b = le.AppendUint32(b, 0x3)  // SPEAKER_FRONT_LEFT | SPEAKER_FRONT_RIGHT
	b = le.AppendUint32(b, sub)
	return append(b, subFormatTail...)
}

func TestParseWaveFormat(t *testing.T) {
	f32 := Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true}
	s16 := Format{SampleRate: 44100, Channels: 2, BitsPerSample: 16}
	badTail := waveFormatEx(waveTagExtensible, 2, 48000, 32, true, waveTagFloat)
	badTail[len(badTail)-1] ^= 0xff

	tests := []struct {
		name string
		b    []byte
		want Format
		ok   bool
	}{
		{name: "extensible float (typical mix format)", b: waveFormatEx(waveTagExtensible, 2, 48000, 32, true, waveTagFloat), want: f32, ok: true},
		{name: "extensible pcm", b: waveFormatEx(waveTagExtensible, 2, 44100, 16, true, waveTagPCM), want: s16, ok: true},
		{name: "plain float", b: waveFormatEx(waveTagFloat, 2, 48000, 32, false, 0), want: f32, ok: true},
		{name: "plain pcm", b: waveFormatEx(waveTagPCM, 2, 44100, 16, false, 0), want: s16, ok: true},
		{name: "short", b: make([]byte, 10)},
		{name: "extensible without tail", b: waveFormatEx(waveTagExtensible, 2, 48000, 32, false, 0)},
		{name: "unknown sub-format GUID", b: badTail},
		{name: "unknown tag", b: waveFormatEx(0x0055, 2, 44100, 16, false, 0)},
		{name: "zero channels", b: waveFormatEx(waveTagPCM, 0, 44100, 16, false, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWaveFormat(tt.b)
			if !tt.ok {
				if !errors.Is(err, errFormat) {
					t.Fatalf("parseWaveFormat = %v, %v; want errFormat", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("parseWaveFormat = %v, %v; want %v", got, err, tt.want)
			}
			if n := waveFormatLen(tt.b); n != len(tt.b) {
				t.Fatalf("waveFormatLen = %d, want %d", n, len(tt.b))
			}
		})
	}
}

func TestEncodeWaveFormatRoundTrip(t *testing.T) {
	for _, f := range processFormats() {
		b := encodeWaveFormat(f)
		if len(b) != waveFormatExSize || waveFormatLen(b) != waveFormatExSize {
			t.Fatalf("%s: %d bytes", f, len(b))
		}
		got, err := parseWaveFormat(b)
		if err != nil || got != f {
			t.Fatalf("%s: round trip = %v, %v", f, got, err)
		}
	}
}

func TestTreeRoot(t *testing.T) {
	procs := []Proc{
		{PID: 4, ParentPID: 0, Exe: "System"},
		{PID: 100, ParentPID: 50, Exe: "explorer.exe"},
		{PID: 200, ParentPID: 100, Exe: "chrome.exe"}, // browser main process
		{PID: 201, ParentPID: 200, Exe: "chrome.exe"},
		{PID: 202, ParentPID: 200, Exe: "chrome.exe"},
		{PID: 203, ParentPID: 201, Exe: "Chrome.EXE"},
		{PID: 300, ParentPID: 100, Exe: "chrome.exe"}, // a second, smaller instance
		{PID: 301, ParentPID: 300, Exe: "chrome.exe"},
		{PID: 400, ParentPID: 999, Exe: "Zoom.exe"}, // parent already gone
		{PID: 500, ParentPID: 501, Exe: "loop.exe"}, // PID reuse can make a cycle
		{PID: 501, ParentPID: 500, Exe: "loop.exe"},
		{PID: 600, ParentPID: 100, Exe: "tie.exe"},
		{PID: 601, ParentPID: 100, Exe: "tie.exe"},
	}
	tests := []struct {
		exe  string
		want uint32
		err  error
	}{
		{exe: "chrome.exe", want: 200},
		{exe: "CHROME.EXE", want: 200},
		{exe: "zoom.exe", want: 400},
		{exe: "explorer.exe", want: 100},
		{exe: "tie.exe", want: 600},
		{exe: "loop.exe", want: 500},
		{exe: "teams.exe", err: ErrNoProcess},
		{exe: "", err: ErrNoProcess},
	}
	for _, tt := range tests {
		t.Run(tt.exe, func(t *testing.T) {
			got, err := TreeRoot(procs, tt.exe)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("TreeRoot = %d, %v; want %v", got, err, tt.err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("TreeRoot = %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}
