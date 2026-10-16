package audiocapture

import (
	"bytes"
	"errors"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

// The parent's timeout covers the child's own budget with a margin for its start, and stays within the 15 s the
// diagnostics page promises.
func TestSelfTestTimeoutCoversTheChild(t *testing.T) {
	b := SelfTestTiming{}.withDefaults()
	child := b.Open + b.Lead + b.Tone + b.Tail
	if DefaultSelfTestTimeout < child+selfTestMargin || selfTestMargin < 3*time.Second {
		t.Fatalf("timeout %v for a child budget of %v: want at least the budget and a margin of 3 s", DefaultSelfTestTimeout, child)
	}
	if DefaultSelfTestTimeout > 15*time.Second {
		t.Fatalf("timeout %v: the page says the check takes up to 15 s", DefaultSelfTestTimeout)
	}
	if DefaultConfig().SelfTestTimeout != DefaultSelfTestTimeout {
		t.Fatal("DefaultConfig must use DefaultSelfTestTimeout")
	}
}

// detect feeds samples to a fresh detector in packets of 10 ms and reports whether it heard the tone.
func detect(rate uint32, samples []float32) bool {
	d := newToneDetector(selfTestFreq, rate)
	step := int(rate) / 100
	for len(samples) > 0 {
		n := min(step, len(samples))
		d.push(samples[:n])
		samples = samples[n:]
	}
	return d.heard
}

func sine(rate uint32, freq, amp float64, d float64) []float32 {
	out := make([]float32, int(float64(rate)*d))
	for i := range out {
		out[i] = float32(amp * math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
	}
	return out
}

func noise(rate uint32, amp float64, d float64, seed uint64) []float32 {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	out := make([]float32, int(float64(rate)*d))
	for i := range out {
		out[i] = float32(amp * (2*r.Float64() - 1))
	}
	return out
}

func mix(a, b []float32) []float32 {
	out := make([]float32, len(a))
	for i := range a {
		out[i] = a[i] + b[i]
	}
	return out
}

func TestToneDetector(t *testing.T) {
	tests := []struct {
		name    string
		rate    uint32
		samples []float32
		want    bool
	}{
		{"tone at 48 kHz", 48000, sine(48000, 1000, 0.25, 1.5), true},
		{"tone at 44.1 kHz", 44100, sine(44100, 1000, 0.25, 1.5), true},
		{"tone at 8 kHz", 8000, sine(8000, 1000, 0.25, 1.5), true},
		{"quiet tone, -50 dBFS", 48000, sine(48000, 1000, 0.003, 1.5), true},
		{"tone under loud noise of the same power", 48000, mix(sine(48000, 1000, 0.25, 1.5), noise(48000, 0.3, 1.5, 1)), true},
		{"silence", 48000, make([]float32, 48000), false},
		{"noise", 48000, noise(48000, 0.5, 1.5, 2), false},
		{"another frequency", 48000, sine(48000, 1500, 0.25, 1.5), false},
		{"a near frequency", 48000, sine(48000, 1100, 0.25, 1.5), false},
		{"too quiet, -70 dBFS", 48000, sine(48000, 1000, 0.0003, 1.5), false},
		{"too short, 200 ms", 48000, sine(48000, 1000, 0.25, 0.2), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detect(tt.rate, tt.samples); got != tt.want {
				t.Fatalf("heard = %v, want %v", got, tt.want)
			}
		})
	}

	d := newToneDetector(selfTestFreq, 48000)
	d.silence(48000)
	if d.heard {
		t.Fatal("silent packets were heard")
	}
}

// The tone a TonePlayer renders decodes back to the tone in every mix format.
func TestSineFramesAreHeard(t *testing.T) {
	for _, f := range []Format{
		{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true},
		{SampleRate: 44100, Channels: 2, BitsPerSample: 16},
		{SampleRate: 48000, Channels: 6, BitsPerSample: 24},
		{SampleRate: 96000, Channels: 2, BitsPerSample: 32},
	} {
		t.Run(f.String(), func(t *testing.T) {
			frames := int(f.SampleRate) // one second in two writes, as the render loop continues from a position
			data := append(sineFrames(f, selfTestFreq, selfTestAmplitude, 0, frames/2),
				sineFrames(f, selfTestFreq, selfTestAmplitude, int64(frames/2), frames-frames/2)...)
			if len(data) != frames*f.BlockAlign() {
				t.Fatalf("%d bytes for %d frames", len(data), frames)
			}
			mono := toMono(f, data, nil)
			if !detect(f.SampleRate, mono) {
				t.Fatal("the rendered tone is not heard")
			}
			if peak := maxAbs(mono); math.Abs(peak-selfTestAmplitude) > 0.01 {
				t.Fatalf("peak %.3f, want -12 dBFS (%.2f)", peak, selfTestAmplitude)
			}
		})
	}
}

func maxAbs(s []float32) float64 {
	var m float64
	for _, x := range s {
		m = max(m, math.Abs(float64(x)))
	}
	return m
}

func TestHandsFreeEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		formFactor uint32
		ok         bool
		enumerator string
		want       bool
	}{
		{"bluetooth headset (HFP)", formFactorHeadset, true, "BTHHFENUM", true},
		{"bluetooth handset", formFactorHandset, true, "BTHENUM", true},
		{"lower case enumerator", formFactorHeadset, true, "bthhfenum", true},
		{"bluetooth headphones (A2DP)", 3, true, "BTHENUM", false},
		{"usb headset", formFactorHeadset, true, "USB", false},
		{"speakers", 1, true, "HDAUDIO", false},
		{"enumerator unknown", formFactorHeadset, true, "", false},
		{"form factor unknown", formFactorHeadset, false, "BTHHFENUM", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := handsFreeEndpoint(tt.formFactor, tt.ok, tt.enumerator); got != tt.want {
				t.Fatalf("handsFreeEndpoint = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelfTestRequest(t *testing.T) {
	line := encodeSelfTest()
	if !bytes.HasSuffix(line, []byte("\n")) || bytes.Count(line, []byte("\n")) != 1 {
		t.Fatalf("request is not one line: %q", line)
	}
	if _, selfTest, err := decodeChildRequest(bytes.TrimSpace(line)); err != nil || !selfTest {
		t.Fatalf("decode = %v, %v", selfTest, err)
	}
	if _, err := decodeRequest(bytes.TrimSpace(line)); !errors.Is(err, ErrProtocol) {
		t.Fatalf("a self-test as a capture request: %v", err)
	}
	capture, err := encodeRequest(validRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, selfTest, err := decodeChildRequest(bytes.TrimSpace(capture)); err != nil || selfTest {
		t.Fatalf("a capture request: self-test %v, %v", selfTest, err)
	}

	for _, bad := range []string{
		`{"v":1,"kind":"selftest","recording":"rec1"}`,
		`{"v":1,"kind":"selftest","chunk_seconds":60}`,
		`{"v":1,"kind":"selftest","target":{"mode":"include","pid":1}}`,
		`{"v":1,"kind":"selftest","tone":"loud"}`,
		`{"v":2,"kind":"selftest"}`,
		`{"v":1,"kind":"play"}`,
		`{"kind":"selftest"}`,
	} {
		if _, _, err := decodeChildRequest([]byte(bad)); !errors.Is(err, ErrProtocol) {
			t.Errorf("decodeChildRequest(%s) = %v, want ErrProtocol", bad, err)
		}
	}
}

func TestDecodeSelfTestEvent(t *testing.T) {
	e, err := decodeEvent([]byte(`{"type":"selftest","mode":"include","cause":"unsupported","hresult":"0x80070005",` +
		`"device":"Headset (WH-1000 Hands-Free AG Audio)","hands_free":true}`))
	if err != nil || e.Cause != "unsupported" || !e.HandsFree || e.HResult != "0x80070005" {
		t.Fatalf("selftest = %+v, %v", e, err)
	}
	for _, bad := range []string{
		`{"type":"selftest","mode":"include","cause":"loud"}`,
		`{"type":"selftest","mode":"include"}`,
		`{"type":"selftest","mode":"mic","cause":"ok"}`,
		`{"type":"selftest","cause":"ok"}`,
		`{"type":"selftest","mode":"system","cause":"ok","samples":[1,2]}`,
		`{"type":"attempt","mode":"include","cause":"ok"}`,
		`{"type":"switch","from":"include","to":"system","cause":"ok"}`,
	} {
		if _, err := decodeEvent([]byte(bad)); !errors.Is(err, ErrProtocol) {
			t.Errorf("decodeEvent(%s) = %v, want ErrProtocol", bad, err)
		}
	}
}
