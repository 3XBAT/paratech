package transcript

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNewDocument(t *testing.T) {
	tests := []struct {
		name    string
		lang    Language
		segs    []Segment
		wantErr bool
	}{
		{name: "empty", segs: nil},
		{name: "valid", lang: "ru", segs: []Segment{{0, 1000, "Привет."}, {1000, 2000, "second"}}},
		{name: "equal starts and zero length", segs: []Segment{{10, 10, "a"}, {10, 20, "b"}}},
		{name: "max offset", segs: []Segment{{MaxOffsetMs, MaxOffsetMs, "x"}}},
		{name: "max text", segs: []Segment{{0, 1, strings.Repeat("x", MaxSegmentTextBytes)}}},
		{name: "bad language", lang: "RU", wantErr: true},
		{name: "negative start", segs: []Segment{{-1, 1, "x"}}, wantErr: true},
		{name: "negative end", segs: []Segment{{0, -1, "x"}}, wantErr: true},
		{name: "end before start", segs: []Segment{{5, 1, "x"}}, wantErr: true},
		{name: "out of range", segs: []Segment{{0, MaxOffsetMs + 1, "x"}}, wantErr: true},
		{name: "decreasing starts", segs: []Segment{{10, 20, "a"}, {5, 30, "b"}}, wantErr: true},
		{name: "text too long", segs: []Segment{{0, 1, strings.Repeat("x", MaxSegmentTextBytes+1)}}, wantErr: true},
		{name: "invalid utf-8", segs: []Segment{{0, 1, "\xff"}}, wantErr: true},
		{name: "newline", segs: []Segment{{0, 1, "a\nb"}}, wantErr: true},
		{name: "nul", segs: []Segment{{0, 1, "a\x00b"}}, wantErr: true},
		{name: "c1 control", segs: []Segment{{0, 1, "a\u0085b"}}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewDocument(tc.lang, tc.segs)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidDocument) {
					t.Fatalf("NewDocument error = %v, want ErrInvalidDocument", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			if d.Language() != tc.lang || len(d.Segments()) != len(tc.segs) {
				t.Fatalf("document = %+v", d)
			}
		})
	}
}

func TestNewDocumentCopiesSegments(t *testing.T) {
	segs := []Segment{{0, 1, "a"}}
	d, err := NewDocument("", segs)
	if err != nil {
		t.Fatal(err)
	}
	segs[0].Text = "changed"
	out := d.Segments()
	out[0].Text = "changed too"
	if got := d.Segments()[0].Text; got != "a" {
		t.Fatalf("document changed from outside: %q", got)
	}
}

func TestDocumentRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		lang Language
		segs []Segment
	}{
		{"with language", "ru", []Segment{{0, 10, `quote " and \ and <tag>`}, {10, 20, "Привет"}}},
		{"empty without language", "", []Segment{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewDocument(tc.lang, tc.segs)
			if err != nil {
				t.Fatal(err)
			}
			data, err := d.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `"segments": [`) {
				t.Errorf("segments must encode as an array: %s", data)
			}
			back, err := ParseDocument(data)
			if err != nil {
				t.Fatalf("ParseDocument: %v\n%s", err, data)
			}
			if back.Language() != tc.lang || !reflect.DeepEqual(back.Segments(), d.Segments()) {
				t.Fatalf("round trip = %+v, want %+v", back, d)
			}
		})
	}
}

func TestParseDocument(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid", input: `{"version": 1, "language": "en", "segments": [{"start_ms": 0, "end_ms": 5, "text": "x"}]}`},
		{name: "no language", input: `{"version": 1, "segments": []}`},
		{name: "empty", input: "", wantErr: true},
		{name: "not json", input: "x", wantErr: true},
		{name: "array", input: "[]", wantErr: true},
		{name: "no version", input: `{"segments": []}`, wantErr: true},
		{name: "other version", input: `{"version": 2, "segments": []}`, wantErr: true},
		{name: "no segments", input: `{"version": 1}`, wantErr: true},
		{name: "null segments", input: `{"version": 1, "segments": null}`, wantErr: true},
		{name: "unknown field", input: `{"version": 1, "segments": [], "extra": 1}`, wantErr: true},
		{name: "unknown segment field", input: `{"version": 1, "segments": [{"start_ms": 0, "end_ms": 1, "text": "", "x": 1}]}`, wantErr: true},
		{name: "missing text", input: `{"version": 1, "segments": [{"start_ms": 0, "end_ms": 1}]}`, wantErr: true},
		{name: "missing end", input: `{"version": 1, "segments": [{"start_ms": 0, "text": ""}]}`, wantErr: true},
		{name: "fractional", input: `{"version": 1, "segments": [{"start_ms": 0.5, "end_ms": 1, "text": ""}]}`, wantErr: true},
		{name: "trailing data", input: `{"version": 1, "segments": []} {}`, wantErr: true},
		{name: "bad language", input: `{"version": 1, "language": "xx-YY", "segments": []}`, wantErr: true},
		{name: "rule of NewDocument", input: `{"version": 1, "segments": [{"start_ms": 5, "end_ms": 1, "text": ""}]}`, wantErr: true},
		{name: "escaped control character", input: `{"version": 1, "segments": [{"start_ms": 0, "end_ms": 1, "text": "a\nb"}]}`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDocument([]byte(tc.input))
			if tc.wantErr != (err != nil) {
				t.Fatalf("ParseDocument error = %v, want error %v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("error %v does not wrap ErrInvalidDocument", err)
			}
		})
	}
}

func TestParseDocumentTooLarge(t *testing.T) {
	data := make([]byte, MaxDocumentBytes+1)
	if _, err := ParseDocument(data); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("error = %v, want ErrInvalidDocument", err)
	}
}
