package values

import (
	"errors"
	"strings"
	"testing"
)

func TestServerPromptOperations(t *testing.T) {
	for op, want := range map[Operation]bool{
		OperationInspectMaterial: true, OperationAnswer: true, OperationTriageAssignment: true,
		OperationSummary: false, OperationLectureText: false, OperationAgentStep: false,
	} {
		if op.ServerPrompt() != want {
			t.Errorf("%s: ServerPrompt = %v", op, !want)
		}
	}
	// AgentStep has its own RPC; it is never a Complete operation.
	if _, err := NewSpec("k", "agent_step", "default", "i", "d", 0); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("agent_step spec: err = %v", err)
	}
}

func TestServerPromptSpec(t *testing.T) {
	for _, alias := range []string{"", "strong"} {
		s, err := NewSpec("k", "answer", alias, "", "chunks", 100)
		if err != nil {
			t.Fatalf("alias %q: %v", alias, err)
		}
		done, err := s.WithServerPrompt("strong", "v1", "Answer from the chunks.")
		if err != nil {
			t.Fatalf("alias %q: %v", alias, err)
		}
		if done.Alias() != "strong" || done.Instructions() != "Answer from the chunks." || done.PromptVersion() != "v1" ||
			done.Data() != "chunks" || done.Operation() != OperationAnswer {
			t.Fatalf("alias %q: spec = %v", alias, done)
		}
	}
	if _, err := NewSpec("k", "inspect_material", "", "client prompt", "d", 0); !errors.Is(err, ErrClientInstructions) ||
		!errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("client instructions: err = %v", err)
	}
	for name, args := range map[string][2]string{"bad alias": {"Strong", ""}, "NUL data": {"", "\x00"}} {
		if _, err := NewSpec("k", "answer", args[0], "", args[1], 0); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := NewSpec("k", "answer", "", "", strings.Repeat("d", MaxPayloadBytes+1), 0); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("large data: err = %v", err)
	}

	other, _ := NewSpec("k", "triage_assignment", "fast", "", "d", 0)
	if _, err := other.WithServerPrompt("strong", "v1", "p"); !errors.Is(err, ErrAliasMismatch) || !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("other alias: err = %v, want ErrAliasMismatch", err)
	}
	client, _ := NewSpec("k", "summary", "default", "i", "d", 0)
	if _, err := client.WithServerPrompt("default", "v1", "p"); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("client operation: err = %v", err)
	}
	s, _ := NewSpec("k", "answer", "", "", "d", 0)
	for name, p := range map[string][3]string{"no version": {"strong", "", "p"}, "no text": {"strong", "v1", ""}, "bad alias": {"", "v1", "p"}} {
		if _, err := s.WithServerPrompt(p[0], p[1], p[2]); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	done, _ := s.WithServerPrompt("strong", "v1", "p")
	if _, err := done.WithServerPrompt("strong", "v1", "p"); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("second server prompt: err = %v", err)
	}
	big, _ := NewSpec("k", "answer", "", "", strings.Repeat("d", MaxPayloadBytes), 0)
	if _, err := big.WithServerPrompt("strong", "v1", "p"); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("prompt over the payload: err = %v", err)
	}
}

// TestServerPromptFingerprint: the prompt version, not its text, is in the fingerprint; a new version is a new request.
func TestServerPromptFingerprint(t *testing.T) {
	spec := func(alias, version, text string) Spec {
		t.Helper()
		s, err := NewSpec("k", "answer", alias, "", "chunks", 0)
		if err == nil {
			s, err = s.WithServerPrompt("strong", version, text)
		}
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	fp := spec("", "v1", "Prompt one.").Fingerprint()
	if spec("strong", "v1", "Prompt one.").Fingerprint() != fp {
		t.Fatal("an empty and the server alias must give the same fingerprint")
	}
	if spec("", "v1", "Prompt one, edited.").Fingerprint() != fp {
		t.Fatal("the prompt text is not part of the fingerprint, its version is")
	}
	if spec("", "v2", "Prompt one.").Fingerprint() == fp {
		t.Fatal("a new prompt version must change the fingerprint")
	}
}
