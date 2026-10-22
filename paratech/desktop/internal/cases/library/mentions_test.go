package library

import (
	"errors"
	"strings"
	"testing"
)

// MentionsEmail reads the reader's stored flag only: no answer, a refused one and an unreadable store answer without
// a call to the reader (TASK-127).
func TestMentionsEmail(t *testing.T) {
	withEmail := strings.Replace(answerOf("homework", ""), `"mentions_email":false`, `"mentions_email":true`, 1)
	tests := []struct {
		name    string
		outputs []string // reader answers of one Inspect; nil: not inspected
		want    bool
	}{
		{"not inspected", nil, false},
		{"mentions an address", []string{withEmail}, true},
		{"no address", []string{answerOf("lecture", "")}, false},
		{"refused answer", []string{`{"doc_kind":"lecture"}`}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			m := importText(t, e, lectureText)
			if tt.outputs != nil {
				e.reader.outputs = tt.outputs
				_, _ = e.svc.Inspect(t.Context(), m.ID()) // a refused answer is an error; its record is what counts
			}
			calls := len(e.reader.requests())
			got, err := e.svc.MentionsEmail(t.Context(), m.ID())
			if err != nil || got != tt.want {
				t.Fatalf("MentionsEmail = %v, %v; want %v", got, err, tt.want)
			}
			if len(e.reader.requests()) != calls {
				t.Fatal("MentionsEmail asked the reader")
			}
		})
	}

	e := newEnv(t)
	e.inspections.err = errors.New("disk")
	if _, err := e.svc.MentionsEmail(t.Context(), "m1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unreadable store = %v", err)
	}
}
