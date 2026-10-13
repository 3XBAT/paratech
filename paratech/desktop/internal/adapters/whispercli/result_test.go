package whispercli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seg(from, to int64, text string) string {
	return fmt.Sprintf(`{"timestamps": {"from": "x", "to": "y"}, "offsets": {"from": %d, "to": %d}, "text": %q}`, from, to, text)
}

func doc(lang string, segs ...string) string {
	return fmt.Sprintf(`{"systeminfo": "AVX = 1", "result": {"language": %q}, "transcription": [%s]}`, lang, strings.Join(segs, ","))
}

func TestParseResult(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Transcript
		wantErr error
	}{
		{
			name:  "valid",
			input: doc("ru", seg(0, 1000, " Привет."), seg(1000, 2000, " second ")),
			want: Transcript{Version: TranscriptFormatVersion, Language: "ru", Segments: []Segment{
				{0, 1000, "Привет."}, {1000, 2000, "second"},
			}},
		},
		{
			name:  "raw control characters in text, as the CLI writes them",
			input: "{\"transcription\": [{\"offsets\": {\"from\": 0, \"to\": 5}, \"text\": \"a\tb\nc\x01d\"}]}",
			want:  Transcript{Version: TranscriptFormatVersion, Segments: []Segment{{0, 5, "a b c d"}}},
		},
		{
			name:  "unknown language is dropped",
			input: doc("klingon", seg(0, 1, "x")),
			want:  Transcript{Version: TranscriptFormatVersion, Segments: []Segment{{0, 1, "x"}}},
		},
		{
			name:  "empty transcription",
			input: doc("en"),
			want:  Transcript{Version: TranscriptFormatVersion, Language: "en", Segments: []Segment{}},
		},
		{
			name:  "equal starts and zero-length segment",
			input: doc("en", seg(10, 10, "a"), seg(10, 20, "b")),
			want:  Transcript{Version: TranscriptFormatVersion, Language: "en", Segments: []Segment{{10, 10, "a"}, {10, 20, "b"}}},
		},
		{name: "not json", input: "not json {", wantErr: ErrOutput},
		{name: "empty file", input: "", wantErr: ErrOutput},
		{name: "array instead of object", input: "[]", wantErr: ErrOutput},
		{name: "truncated", input: `{"transcription": [`, wantErr: ErrOutput},
		{name: "trailing object", input: doc("en") + " {}", wantErr: ErrOutput},
		{name: "trailing garbage", input: doc("en") + " x", wantErr: ErrOutput},
		{name: "no transcription", input: `{"result": {"language": "en"}}`, wantErr: ErrOutput},
		{name: "null transcription", input: `{"transcription": null}`, wantErr: ErrOutput},
		{name: "missing offsets", input: `{"transcription": [{"text": "x"}]}`, wantErr: ErrOutput},
		{name: "missing to", input: `{"transcription": [{"offsets": {"from": 0}, "text": "x"}]}`, wantErr: ErrOutput},
		{name: "missing text", input: `{"transcription": [{"offsets": {"from": 0, "to": 1}}]}`, wantErr: ErrOutput},
		{name: "string offset", input: `{"transcription": [{"offsets": {"from": "0", "to": 1}, "text": "x"}]}`, wantErr: ErrOutput},
		{name: "fractional offset", input: `{"transcription": [{"offsets": {"from": 0.5, "to": 1}, "text": "x"}]}`, wantErr: ErrOutput},
		{name: "negative", input: doc("en", seg(-10, 5, "x")), wantErr: ErrOutput},
		{name: "start after end", input: doc("en", seg(5000, 1000, "x")), wantErr: ErrOutput},
		{name: "out of range", input: doc("en", seg(0, maxOffsetMs+1, "x")), wantErr: ErrOutput},
		{name: "out of order", input: doc("en", seg(2000, 3000, "a"), seg(1000, 1500, "b")), wantErr: ErrOutput},
		{name: "text too long", input: doc("en", seg(0, 1, strings.Repeat("x", maxSegmentText+1))), wantErr: ErrOutput},
		{name: "huge number", input: `{"transcription": [{"offsets": {"from": 0, "to": 1e30}, "text": "x"}]}`, wantErr: ErrOutput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseResult([]byte(tt.input))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseResult: %v", err)
			}
			if got.Version != tt.want.Version || got.Language != tt.want.Language || len(got.Segments) != len(tt.want.Segments) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got.Segments {
				if got.Segments[i] != tt.want.Segments[i] {
					t.Errorf("segment %d = %+v, want %+v", i, got.Segments[i], tt.want.Segments[i])
				}
			}
		})
	}
}

func TestReadResultLimits(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.json")
	big := filepath.Join(dir, "big.json")
	if err := os.WriteFile(small, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(big, make([]byte, 2048), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{"within the limit", small, nil},
		{"over the limit", big, ErrOutputTooLarge},
		{"missing", filepath.Join(dir, "none.json"), os.ErrNotExist},
		{"directory", dir, ErrOutput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readResult(tt.path, 1024)
			if tt.wantErr == nil && err != nil || tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestEncodeTranscriptRoundTrip(t *testing.T) {
	in := Transcript{Version: TranscriptFormatVersion, Language: "ru", Segments: []Segment{{0, 10, `quote " and \ and <tag>`}}}
	data, err := encodeTranscript(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"start_ms": 0`) || !strings.Contains(string(data), `"segments"`) {
		t.Errorf("unexpected encoding: %s", data)
	}
}
