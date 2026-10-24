package shared

import (
	"errors"
	"testing"
)

func TestAgentIDValidate(t *testing.T) {
	validators := map[string]func(string) error{
		"ThreadID":    func(s string) error { return ThreadID(s).Validate() },
		"ActionID":    func(s string) error { return ActionID(s).Validate() },
		"WorkID":      func(s string) error { return WorkID(s).Validate() },
		"AnswerID":    func(s string) error { return AnswerID(s).Validate() },
		"TicketSetID": func(s string) error { return TicketSetID(s).Validate() },
	}
	cases := []struct {
		name    string
		id      string
		invalid bool
	}{
		{"token", "0b9d6f3e4c1a4d6e", false},
		{"empty", "", true},
		{"space", "a b", true},
		{"colon", "a:b", true},
		{"reserved name", "NUL", true},
	}
	for typ, validate := range validators {
		for _, tc := range cases {
			t.Run(typ+"/"+tc.name, func(t *testing.T) {
				err := validate(tc.id)
				if tc.invalid != (err != nil) {
					t.Fatalf("Validate(%q) error = %v, want invalid=%v", tc.id, err, tc.invalid)
				}
				if err != nil && !errors.Is(err, ErrInvalidID) {
					t.Fatalf("error %v does not wrap ErrInvalidID", err)
				}
			})
		}
	}
}

func TestNameSegmentProblem(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"plain", "Лекции 2026", true},
		{"slash", "a/b", false},
		{"trailing dot", "abc.", false},
		{"trailing space", "abc ", false},
		{"device name", "con", false},
		{"control", "a\tb", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NameSegmentProblem(tt.in); (got == "") != tt.ok {
				t.Fatalf("NameSegmentProblem(%q) = %q, want ok=%v", tt.in, got, tt.ok)
			}
		})
	}
}
