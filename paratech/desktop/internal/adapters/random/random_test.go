package random

import (
	"context"
	"strings"
	"testing"
)

const draws = 200

func TestIDsAreValidAndDistinct(t *testing.T) {
	g := IDs{}
	tests := []struct {
		name string
		next func() (string, error)
	}{
		{"recording", func() (string, error) { id := g.NewRecordingID(); return string(id), id.Validate() }},
		{"job", func() (string, error) { id := g.NewJobID(); return string(id), id.Validate() }},
		{"assignment", func() (string, error) { id := g.NewAssignmentID(); return string(id), id.Validate() }},
		{"proposal", func() (string, error) { id := g.NewProposalID(); return string(id), id.Validate() }},
		{"delivery", func() (string, error) { id := g.NewDeliveryID(); return string(id), id.Validate() }},
		{"lesson", func() (string, error) { id := g.NewLessonID(); return string(id), id.Validate() }},
		{"stt model", func() (string, error) { id := g.NewSttModelID(); return string(id), id.Validate() }},
		{"agent thread", func() (string, error) { id := g.NewThreadID(); return string(id), id.Validate() }},
		{"agent action", func() (string, error) { id := g.NewActionID(); return string(id), id.Validate() }},
		{"subject", func() (string, error) { id := g.NewSubjectID(); return string(id), id.Validate() }},
		{"folder", func() (string, error) { id := g.NewFolderID(); return string(id), id.Validate() }},
		{"material", func() (string, error) { id := g.NewMaterialID(); return string(id), id.Validate() }},
		{"work", func() (string, error) { id := g.NewWorkID(); return string(id), id.Validate() }},
		{"answer", func() (string, error) { id := g.NewAnswerID(); return string(id), id.Validate() }},
		{"ticket set", func() (string, error) { id := g.NewTicketSetID(); return string(id), id.Validate() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen := map[string]bool{}
			for range draws {
				id, err := tt.next()
				if err != nil {
					t.Fatalf("id %q: %v", id, err)
				}
				if len(id) < 26 || strings.ToLower(id) != id || strings.Trim(id, "abcdefghijklmnopqrstuvwxyz234567") != "" {
					t.Fatalf("id %q: want at least 26 lower-case base32 characters", id)
				}
				if seen[id] {
					t.Fatalf("id %q repeated", id)
				}
				seen[id] = true
			}
		})
	}
}

func TestNewMessageID(t *testing.T) {
	seen := map[string]bool{}
	for range draws {
		id := IDs{}.NewMessageID()
		inner, ok := strings.CutPrefix(id, "<")
		if ok {
			inner, ok = strings.CutSuffix(inner, ">")
		}
		left, right, found := strings.Cut(inner, "@")
		if !ok || !found || len(left) < 26 || right != messageIDDomain || strings.ContainsAny(left, "<>@ ") {
			t.Fatalf("message id %q: want <random@%s>", id, messageIDDomain)
		}
		if seen[id] {
			t.Fatalf("message id %q repeated", id)
		}
		seen[id] = true
	}
}

func TestPermille(t *testing.T) {
	values := map[int]bool{}
	for range draws {
		n, err := Jitter{}.Permille(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n < 0 || n >= permilleRange {
			t.Fatalf("Permille = %d, want 0..999", n)
		}
		values[n] = true
	}
	// 200 uniform draws from 1000 values: fewer than 100 distinct ones means the source is broken.
	if len(values) < 100 {
		t.Fatalf("%d distinct values in %d draws: not random", len(values), draws)
	}
}
