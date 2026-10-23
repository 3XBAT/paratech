package values

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestWorkJobPayload(t *testing.T) {
	for _, tt := range []struct {
		work, assignment, want string
	}{
		{"w1", "a1", `{"work_id":"w1","assignment_id":"a1"}`},
		{"w2", "", `{"work_id":"w2"}`},
	} {
		j, err := NewWorkJob(shared.WorkID(tt.work), shared.AssignmentID(tt.assignment))
		if err != nil || j.Payload() != tt.want {
			t.Fatalf("payload = %s, %v", j.Payload(), err)
		}
		got, err := ParseWorkJob(j.Payload())
		if err != nil || got != j {
			t.Fatalf("parse = %+v, %v", got, err)
		}
	}
	for _, bad := range []string{
		`{}`, `{"work_id":""}`, `{"work_id":"w1","assignment_id":""}`, `{"work_id":"w1","x":1}`, `{"work_id":"w1"} {}`,
		`{"work_id":"a/b"}`, `[]`,
	} {
		if _, err := ParseWorkJob(bad); !errors.Is(err, ErrInvalidWorkJob) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if WorkCheckpoint(2, 5) != "2/5" {
		t.Fatal(WorkCheckpoint(2, 5))
	}
}
