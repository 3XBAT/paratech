package audiocapture

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
)

func TestLevelCheckRequest(t *testing.T) {
	req := LevelRequest{Target: Target{Mode: ModeInclude, PID: 300}, Duration: 15 * time.Second}
	line, err := encodeLevelCheck(req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(line, []byte("\n")) || bytes.Count(line, []byte("\n")) != 1 {
		t.Fatalf("request is not one line: %q", line)
	}
	want := `{"v":1,"kind":"levelcheck","target":{"mode":"include","pid":300},"duration_ms":15000}`
	if got := string(bytes.TrimSpace(line)); got != want {
		t.Fatalf("line %s\nwant %s", got, want)
	}
	_, lc, level, err := decodeChildRequest(bytes.TrimSpace(line))
	if err != nil || !level || lc != req {
		t.Fatalf("decode = %+v, %v, %v", lc, level, err)
	}
	if _, err := decodeRequest(bytes.TrimSpace(line)); !errors.Is(err, ErrProtocol) {
		t.Fatalf("a level check as a capture request: %v", err)
	}
	capture, err := encodeRequest(validRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, level, err := decodeChildRequest(bytes.TrimSpace(capture)); err != nil || level {
		t.Fatalf("a capture request: level %v, %v", level, err)
	}

	for _, bad := range []LevelRequest{
		{Target: Target{Mode: ModeInclude}, Duration: time.Second},
		{Target: Target{Mode: ModeSystem, PID: 4}, Duration: time.Second},
		{Target: Target{Mode: "mic"}, Duration: time.Second},
		{Target: Target{Mode: ModeSystem}},
		{Target: Target{Mode: ModeSystem}, Duration: -time.Second},
		{Target: Target{Mode: ModeSystem}, Duration: MaxCheckDuration + time.Millisecond},
		{Target: Target{Mode: ModeSystem}, Duration: time.Second + time.Microsecond},
	} {
		if _, err := encodeLevelCheck(bad); !errors.Is(err, ErrProtocol) {
			t.Errorf("encodeLevelCheck(%+v) = %v, want ErrProtocol", bad, err)
		}
	}

	for _, bad := range []string{
		`{"v":1,"kind":"levelcheck","target":{"mode":"system","pid":0}}`,
		`{"v":1,"kind":"levelcheck","target":{"mode":"system","pid":0},"duration_ms":0}`,
		`{"v":1,"kind":"levelcheck","target":{"mode":"system","pid":0},"duration_ms":-5}`,
		`{"v":1,"kind":"levelcheck","target":{"mode":"system","pid":0},"duration_ms":60001}`,
		`{"v":1,"kind":"levelcheck","target":{"mode":"include","pid":0},"duration_ms":1000}`,
		`{"v":1,"kind":"levelcheck","target":{"mode":"exclude","pid":0},"duration_ms":1000}`,
		`{"v":1,"kind":"levelcheck","target":{"mode":"loud","pid":1},"duration_ms":1000}`,
		`{"v":1,"kind":"levelcheck","recording":"rec1","target":{"mode":"system","pid":0},"duration_ms":1000}`,
		`{"v":1,"kind":"levelcheck","chunks":"recordings/rec1","target":{"mode":"system","pid":0},"duration_ms":1000}`,
		`{"v":1,"kind":"levelcheck","chunk_seconds":30,"target":{"mode":"system","pid":0},"duration_ms":1000}`,
		`{"v":1,"kind":"levelcheck","lead_silence_ms":5,"target":{"mode":"system","pid":0},"duration_ms":1000}`,
		`{"v":1,"kind":"levelcheck","floor":"system","target":{"mode":"system","pid":0},"duration_ms":1000}`,
		`{"v":1,"kind":"levelcheck","target":{"mode":"system","pid":0},"duration_ms":1000,"tone":"loud"}`,
		`{"v":2,"kind":"levelcheck","target":{"mode":"system","pid":0},"duration_ms":1000}`,
		`{"kind":"levelcheck","target":{"mode":"system","pid":0},"duration_ms":1000}`,
		`{"v":1,"kind":"selftest"}`,
		`{"v":1,"kind":"play"}`,
		`{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"system","pid":0},"chunk_seconds":30,` +
			`"lead_silence_ms":0,"duration_ms":1000}`,
	} {
		if _, _, _, err := decodeChildRequest([]byte(bad)); !errors.Is(err, ErrProtocol) {
			t.Errorf("decodeChildRequest(%s) = %v, want ErrProtocol", bad, err)
		}
	}
}

func TestDecodeLevelEvent(t *testing.T) {
	for _, line := range []string{
		`{"type":"level","level":0}`, `{"type":"level","level":0.25}`, `{"type":"level","level":1}`,
	} {
		e, err := decodeEvent([]byte(line))
		if err != nil || e.Level == nil {
			t.Errorf("decodeEvent(%s) = %+v, %v", line, e, err)
		}
	}
	e, err := decodeEvent([]byte(`{"type":"failed","cause":"no_device","reason":"open capture: no output device"}`))
	if err != nil || e.Cause != "no_device" {
		t.Fatalf("failed = %+v, %v", e, err)
	}
	for _, bad := range []string{
		`{"type":"level"}`,
		`{"type":"level","level":1.5}`,
		`{"type":"level","level":-0.1}`,
		`{"type":"level","level":1e400}`,
		`{"type":"level","level":"0.5"}`,
		`{"type":"stats","level":0.5}`,
		`{"type":"stopped","level":0}`,
		`{"type":"failed","cause":"loud"}`,
		`{"type":"failed","cause":"activation_failed"}`,
		`{"type":"selftest","mode":"include","cause":"ok"}`,
		`{"type":"level","level":0.5,"samples":[1,2]}`,
	} {
		if _, err := decodeEvent([]byte(bad)); !errors.Is(err, ErrProtocol) {
			t.Errorf("decodeEvent(%s) = %v, want ErrProtocol", bad, err)
		}
	}
}

func TestLevelMeter(t *testing.T) {
	var m levelMeter
	if got := m.take(); got != 0 {
		t.Fatalf("an empty window = %v", got)
	}
	m.add(0.2)
	m.add(0.6)
	m.add(0.1)
	if got := m.take(); got != 0.6 {
		t.Fatalf("take = %v, want the peak 0.6", got)
	}
	if got := m.take(); got != 0 {
		t.Fatalf("the next window = %v, want 0", got)
	}
	m.add(3)
	if got := m.take(); got != 1 {
		t.Fatalf("an overload = %v, want 1", got)
	}
	m.add(math.NaN())
	if got := m.take(); got != 0 {
		t.Fatalf("NaN = %v, want 0", got)
	}
}

func TestSamplePeak(t *testing.T) {
	tests := []struct {
		in   []float32
		want float64
	}{
		{nil, 0}, {[]float32{0, 0}, 0}, {[]float32{0.1, -0.7, 0.3}, 0.7}, {[]float32{-1}, 1},
	}
	for _, tt := range tests {
		if got := samplePeak(tt.in); math.Abs(got-tt.want) > 1e-6 {
			t.Errorf("samplePeak(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestLevelCheckCauses(t *testing.T) {
	opens := []struct {
		err  error
		want values.SoundCause
	}{
		{ErrUnavailable, values.SoundUnsupported},
		{fmt.Errorf("x: %w", recording.ErrCaptureUnavailable), values.SoundUnsupported},
		{ErrNoDevice, values.SoundNoDevice},
		{ErrDevice, values.SoundFailed},
		{errors.New("boom"), values.SoundFailed},
	}
	for _, tt := range opens {
		if got := openCause(tt.err); got != tt.want {
			t.Errorf("openCause(%v) = %s, want %s", tt.err, got, tt.want)
		}
	}

	fails := []struct {
		ev   event
		want values.SoundCause
	}{
		{event{Type: evFailed, Unavailable: true}, values.SoundUnsupported},
		{event{Type: evFailed, Cause: "no_device"}, values.SoundNoDevice},
		{event{Type: evFailed, Cause: "unsupported"}, values.SoundUnsupported},
		{event{Type: evFailed, Cause: "failed"}, values.SoundFailed},
		{event{Type: evFailed, Cause: "ok"}, values.SoundFailed},
		{event{Type: evFailed, Cause: "busy"}, values.SoundFailed},
		{event{Type: evFailed}, values.SoundFailed},
	}
	for _, tt := range fails {
		if got := failedCause(tt.ev); got != tt.want {
			t.Errorf("failedCause(%+v) = %s, want %s", tt.ev, got, tt.want)
		}
	}
}
