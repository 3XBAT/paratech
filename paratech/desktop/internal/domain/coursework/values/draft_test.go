package values

import (
	"errors"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestNewDraftRequest(t *testing.T) {
	r, err := NewDraftRequest("a1", 2, "  shorter, please \n")
	if err != nil {
		t.Fatal(err)
	}
	if r.Assignment() != "a1" || r.Base() != 2 || r.Instruction() != "shorter, please" {
		t.Fatalf("request = %+v", r)
	}
	if got := r.Payload(); got != `{"assignment_id":"a1","base":2,"instruction":"shorter, please"}` {
		t.Fatalf("payload = %s", got)
	}
	back, err := ParseDraftPayload(r.Payload())
	if err != nil || back != r {
		t.Fatalf("round trip = %+v, %v", back, err)
	}

	cases := []struct {
		name        string
		a           string
		base        int
		instruction string
	}{
		{"empty assignment", "", 0, ""},
		{"assignment with a separator", "a/b", 0, ""},
		{"negative base", "a1", -1, ""},
		{"instruction not UTF-8", "a1", 0, "\xff"},
		{"instruction with NUL", "a1", 0, "a\x00b"},
		{"instruction too large", "a1", 0, strings.Repeat("я", MaxInstructionBytes/2+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewDraftRequest(shared.AssignmentID(tc.a), tc.base, tc.instruction); !errors.Is(err, ErrInvalidDraftRequest) {
				t.Fatalf("err = %v, want ErrInvalidDraftRequest", err)
			}
		})
	}
	if _, err := NewDraftRequest("a1", 0, strings.Repeat("x", MaxInstructionBytes)+"\t\n"); err != nil {
		t.Fatalf("instruction at the limit: %v", err)
	}
}

// TASK-094: bidi controls are removed from the instructions; the rest of the text stays as typed.
func TestCleanInstructionStripsBidi(t *testing.T) {
	for in, want := range map[string]string{
		"\u202eshorter\u202c":             "shorter",
		"\u2066титульный\u2069 лист":      "титульный лист",
		"a\u200fb\u061cc":                 "abc",
		"plan:\n1. intro\n2. body \u200d": "plan:\n1. intro\n2. body \u200d",
	} {
		got, err := CleanInstruction(in)
		if err != nil || got != want {
			t.Errorf("CleanInstruction(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestParseDraftPayloadIsStrict(t *testing.T) {
	for _, bad := range []string{
		``, `[]`, `{}`, `null`,
		`{"assignment_id":"a1","base":0}`,
		`{"assignment_id":"a1","instruction":""}`,
		`{"base":0,"instruction":""}`,
		`{"assignment_id":"a1","base":0,"instruction":"","send_to":"x@example.com"}`,
		`{"assignment_id":"a1","base":0,"instruction":""} {}`,
		`{"assignment_id":"a1","base":"1","instruction":""}`,
		`{"assignment_id":"a1","base":1.5,"instruction":""}`,
		`{"assignment_id":"..","base":0,"instruction":""}`,
	} {
		if _, err := ParseDraftPayload(bad); !errors.Is(err, ErrInvalidDraftRequest) {
			t.Errorf("ParseDraftPayload(%s) = %v, want ErrInvalidDraftRequest", bad, err)
		}
	}
}
