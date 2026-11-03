package wails

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/recording/prompt"
)

// TASK-370: the popup gets only the subject id of «Подключиться», under the JSON name the page reads.
func TestPromptServiceCallSubject(t *testing.T) {
	tests := []struct {
		name string
		id   string
		uc   *fakePrompt
		want string
	}{
		{name: "subject with a link", id: "prompt-1", uc: &fakePrompt{callSubject: "sub-1"}, want: `{"subjectId":"sub-1"}`},
		{name: "none to offer", id: "prompt-1", uc: &fakePrompt{}, want: `{"subjectId":""}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewPromptService(tt.uc, discard()).CallSubject(context.Background(), tt.id)
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tt.want {
				t.Fatalf("JSON = %s, want %s", raw, tt.want)
			}
			if !slices.Equal(tt.uc.gotIDs, []prompt.ID{prompt.ID(tt.id)}) {
				t.Fatalf("ids %v", tt.uc.gotIDs)
			}
		})
	}
}

// An id the page could not have got from Current never reaches the use case, and the answer is empty, not an error.
func TestPromptServiceCallSubjectInvalidIDs(t *testing.T) {
	uc := &fakePrompt{callSubject: "sub-1"}
	s := NewPromptService(uc, discard())
	for _, id := range []string{"", "../x", "a b", strings.Repeat("p", 65)} {
		if got := s.CallSubject(context.Background(), id); got != (PromptCallSubject{}) {
			t.Errorf("CallSubject(%q) = %+v, want none", id, got)
		}
	}
	if len(uc.calls) != 0 {
		t.Fatalf("invalid IDs reached the use case: %v", uc.calls)
	}
}
