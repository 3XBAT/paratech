package audiocapture

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func validRequest(t *testing.T) Request {
	t.Helper()
	return Request{
		Recording: "rec1", Chunks: mustRef(t, "rec1"), Target: Target{Mode: ModeExclude, PID: 4242},
		ChunkSeconds: 60, LeadSilence: 1500 * time.Millisecond, Floor: ModeSystem,
	}
}

func TestRequestRoundTrip(t *testing.T) {
	req := validRequest(t)
	line, err := encodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(line, []byte("\n")) || bytes.Count(line, []byte("\n")) != 1 {
		t.Fatalf("request is not one line: %q", line)
	}
	got, err := decodeRequest(bytes.TrimSuffix(line, []byte("\n")))
	if err != nil || got != req {
		t.Fatalf("decode = %+v, %v; want %+v", got, err, req)
	}
	if got.effectiveTarget() != (Target{Mode: ModeSystem}) {
		t.Fatalf("a system floor opens %+v", got.effectiveTarget())
	}
}

func TestDecodeRequestRejects(t *testing.T) {
	base := `"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"exclude","pid":7},"chunk_seconds":60,"lead_silence_ms":0`
	tests := []struct {
		name, line string
	}{
		{"not json", `hello`},
		{"empty", ``},
		{"unknown field", `{"v":1,` + base + `,"shell":"cmd"}`},
		{"unknown target field", `{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"exclude","pid":7,"x":1},"chunk_seconds":60,"lead_silence_ms":0}`},
		{"trailing data", `{"v":1,` + base + `} {}`},
		{"other version", `{"v":2,` + base + `}`},
		{"no version", `{` + base + `}`},
		{"bad recording id", `{"v":1,"recording":"../x","chunks":"recordings/rec1","target":{"mode":"exclude","pid":7},"chunk_seconds":60,"lead_silence_ms":0}`},
		{"chunks of another kind", `{"v":1,"recording":"rec1","chunks":"models/rec1","target":{"mode":"exclude","pid":7},"chunk_seconds":60,"lead_silence_ms":0}`},
		{"chunks leave the root", `{"v":1,"recording":"rec1","chunks":"recordings/../x","target":{"mode":"exclude","pid":7},"chunk_seconds":60,"lead_silence_ms":0}`},
		{"unknown mode", `{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"mic","pid":7},"chunk_seconds":60,"lead_silence_ms":0}`},
		{"include without pid", `{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"include","pid":0},"chunk_seconds":60,"lead_silence_ms":0}`},
		{"system with pid", `{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"system","pid":7},"chunk_seconds":60,"lead_silence_ms":0}`},
		{"negative pid", `{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"exclude","pid":-1},"chunk_seconds":60,"lead_silence_ms":0}`},
		{"short chunks", `{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"exclude","pid":7},"chunk_seconds":29,"lead_silence_ms":0}`},
		{"long chunks", `{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"exclude","pid":7},"chunk_seconds":61,"lead_silence_ms":0}`},
		{"negative lead", `{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"exclude","pid":7},"chunk_seconds":60,"lead_silence_ms":-1}`},
		{"long lead", `{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"exclude","pid":7},"chunk_seconds":60,"lead_silence_ms":60001}`},
		{"huge lead", `{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"exclude","pid":7},"chunk_seconds":60,"lead_silence_ms":9223372036854775807}`},
		{"floor of another mode", `{"v":1,` + base + `,"floor":"include"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if r, err := decodeRequest([]byte(tt.line)); !errors.Is(err, ErrProtocol) {
				t.Fatalf("decodeRequest = %+v, %v; want ErrProtocol", r, err)
			}
		})
	}

	if _, err := decodeRequest([]byte(`{"v":1,` + base + `,"floor":"exclude"}`)); err != nil {
		t.Fatalf("a floor equal to the target mode is allowed: %v", err)
	}
}

func TestLineReader(t *testing.T) {
	input := "first\r\n" + strings.Repeat("x", 50) + "\nsecond\n" + strings.Repeat("y", 70) + "\nlast"
	lr := newLineReader(strings.NewReader(input), 16)

	type res struct {
		line      string
		oversized bool
	}
	var got []res
	for {
		line, oversized, err := lr.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, res{string(line), oversized})
	}
	want := []res{{"first", false}, {"", true}, {"second", false}, {"", true}, {"last", false}}
	if len(got) != len(want) {
		t.Fatalf("lines = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// A long line without a newline at the end is skipped, then EOF.
	lr = newLineReader(strings.NewReader(strings.Repeat("z", 100)), 16)
	if _, oversized, err := lr.next(); err != nil || !oversized {
		t.Fatalf("long last line = %v, %v", oversized, err)
	}
	if _, _, err := lr.next(); !errors.Is(err, io.EOF) {
		t.Fatalf("after the long line: %v", err)
	}
}

func TestDecodeEvent(t *testing.T) {
	if e, err := decodeEvent([]byte(`{"type":"chunk","n":3,"frames":480000,"rate":16000}`)); err != nil || e.N != 3 || e.Frames != 480000 {
		t.Fatalf("chunk event = %+v, %v", e, err)
	}
	for _, bad := range []string{`{"type":"audio","data":"..."}`, `{"type":"boom"}`, `{"type":"stats","extra":1}`, `[]`, `{"type":"stats"} x`} {
		if _, err := decodeEvent([]byte(bad)); !errors.Is(err, ErrProtocol) {
			t.Errorf("decodeEvent(%s) = %v, want ErrProtocol", bad, err)
		}
	}
}

func TestShortText(t *testing.T) {
	if got := shortText("a\nb\x00c"); got != "a b c" {
		t.Fatalf("control characters: %q", got)
	}
	long := strings.Repeat("ж", 400) // 800 bytes
	got := shortText(long)
	if len(got) > maxTextBytes+3 || !strings.HasSuffix(got, "...") || !strings.HasPrefix(long, strings.TrimSuffix(got, "...")) {
		t.Fatalf("truncated text %q (%d bytes) is not a valid prefix", got, len(got))
	}
}
