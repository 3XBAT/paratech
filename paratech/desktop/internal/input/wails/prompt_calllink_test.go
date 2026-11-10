package wails

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/domain/recording/prompt"
)

// TASK-370, ADR-0032 addendum 2026-11-10: the popup gets only the subject id of «Подключиться» and the lesson's kind,
// under the JSON names the page reads.
func TestPromptServiceCallSubject(t *testing.T) {
	tests := []struct {
		name string
		id   string
		uc   *fakePrompt
		want string
	}{
		{
			name: "lecture with a link", id: "prompt-1",
			uc:   &fakePrompt{callSubject: recordingprompt.Call{Subject: "sub-1", LessonKind: "lecture"}},
			want: `{"subjectId":"sub-1","lessonKind":"lecture"}`,
		},
		{
			name: "unknown kind", id: "prompt-1",
			uc:   &fakePrompt{callSubject: recordingprompt.Call{Subject: "sub-1", LessonKind: "unknown"}},
			want: `{"subjectId":"sub-1","lessonKind":"unknown"}`,
		},
		{name: "none to offer", id: "prompt-1", uc: &fakePrompt{}, want: `{"subjectId":"","lessonKind":""}`},
		{
			name: "a kind without a subject is none", id: "prompt-1",
			uc:   &fakePrompt{callSubject: recordingprompt.Call{LessonKind: "seminar"}},
			want: `{"subjectId":"","lessonKind":""}`,
		},
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
	uc := &fakePrompt{callSubject: recordingprompt.Call{Subject: "sub-1", LessonKind: "lecture"}}
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
