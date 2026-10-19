package values

import (
	"errors"
	"testing"
)

func TestSTTRequestRoundTrip(t *testing.T) {
	r, err := NewSTTRequest("rec-1")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.Payload(), `{"recording_id":"rec-1"}`; got != want {
		t.Fatalf("Payload = %s, want %s", got, want)
	}
	back, err := ParseSTTPayload(r.Payload())
	if err != nil || back != r {
		t.Fatalf("ParseSTTPayload = %+v, %v; want %+v", back, err, r)
	}
}

func TestParseSTTPayload(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "valid", input: `{"recording_id": "r1"}`, want: "r1"},
		{name: "empty", input: "", wantErr: true},
		{name: "empty object", input: "{}", wantErr: true},
		{name: "empty id", input: `{"recording_id": ""}`, wantErr: true},
		{name: "path in id", input: `{"recording_id": "../r1"}`, wantErr: true},
		{name: "number", input: `{"recording_id": 1}`, wantErr: true},
		{name: "unknown field", input: `{"recording_id": "r1", "model": "m"}`, wantErr: true},
		{name: "trailing data", input: `{"recording_id": "r1"} {}`, wantErr: true},
		{name: "array", input: `["r1"]`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSTTPayload(tc.input)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidSTTRequest) {
					t.Fatalf("error = %v, want ErrInvalidSTTRequest", err)
				}
				return
			}
			if err != nil || string(got.Recording()) != tc.want {
				t.Fatalf("ParseSTTPayload = %+v, %v; want %s", got, err, tc.want)
			}
		})
	}
}

func TestNewSTTRequestRejectsInvalidID(t *testing.T) {
	if _, err := NewSTTRequest("a b"); !errors.Is(err, ErrInvalidSTTRequest) {
		t.Fatalf("error = %v, want ErrInvalidSTTRequest", err)
	}
}

func TestSTTJobKeyPrefix(t *testing.T) {
	if got := STTJobKeyPrefix("rec-1"); got != "stt:rec-1:" {
		t.Fatalf("STTJobKeyPrefix = %q, want %q", got, "stt:rec-1:")
	}
}

func TestSTTEnginePercent(t *testing.T) {
	tests := []struct {
		job, want int
	}{
		{-5, 0}, {0, 0}, {45, 50}, {STTEngineShare, 100}, {95, 100}, {100, 100},
	}
	for _, tt := range tests {
		if got := STTEnginePercent(tt.job); got != tt.want {
			t.Errorf("STTEnginePercent(%d) = %d, want %d", tt.job, got, tt.want)
		}
	}
}
