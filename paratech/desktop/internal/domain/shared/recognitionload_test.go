package shared

import (
	"slices"
	"testing"
)

func TestRecognitionLoad(t *testing.T) {
	tests := []struct {
		in    RecognitionLoad
		valid bool
		want  RecognitionLoad
	}{
		{LoadGentle, true, LoadGentle},
		{LoadNormal, true, LoadNormal},
		{LoadMax, true, LoadMax},
		{"", false, LoadGentle},
		{"Gentle", false, LoadGentle},
		{"turbo", false, LoadGentle},
	}
	for _, tt := range tests {
		if got := tt.in.Valid(); got != tt.valid {
			t.Errorf("%q.Valid() = %v, want %v", tt.in, got, tt.valid)
		}
		if got := tt.in.OrDefault(); got != tt.want {
			t.Errorf("%q.OrDefault() = %q, want %q", tt.in, got, tt.want)
		}
	}

	if DefaultRecognitionLoad != LoadGentle {
		t.Errorf("default = %q, want gentle", DefaultRecognitionLoad)
	}
	if got := RecognitionLoads(); !slices.Equal(got, []RecognitionLoad{LoadGentle, LoadNormal, LoadMax}) {
		t.Errorf("RecognitionLoads = %v", got)
	}
}
