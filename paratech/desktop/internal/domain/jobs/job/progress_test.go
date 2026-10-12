package job

import (
	"errors"
	"strings"
	"testing"
)

func TestNewProgress(t *testing.T) {
	cases := []struct {
		name       string
		percent    int
		checkpoint string
		invalid    bool
	}{
		{"zero", 0, "", false},
		{"hundred", 100, "", false},
		{"with checkpoint", 40, "chunk=12", false},
		{"max checkpoint", 1, strings.Repeat("x", MaxCheckpointBytes), false},
		{"negative", -1, "", true},
		{"over hundred", 101, "", true},
		{"checkpoint too long", 1, strings.Repeat("x", MaxCheckpointBytes+1), true},
		{"invalid utf8", 1, "\xff\xfe", true},
		{"nul", 1, "a\x00b", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewProgress(tc.percent, tc.checkpoint)
			if tc.invalid {
				if !errors.Is(err, ErrInvalidProgress) {
					t.Fatalf("error = %v, want ErrInvalidProgress", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Percent() != tc.percent || p.Checkpoint() != tc.checkpoint {
				t.Fatalf("got %d %q", p.Percent(), p.Checkpoint())
			}
		})
	}
}
