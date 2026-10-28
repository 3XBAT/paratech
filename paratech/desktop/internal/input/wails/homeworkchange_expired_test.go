package wails

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// «Вернуть» after the restore window answers expired (ADR-0029 addendum 2); the page shows its own text (TASK-275).
// A purged one is gone: not_found.
func TestRestoreAssignmentExpired(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"window ended", assignment.ErrRestoreExpired, "expired"},
		{"purged", shared.ErrNotFound, "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newHWEnv()
			e.proposals.restoreErr = fmt.Errorf("homeworkflow: restore assignment a1: %w", tc.err)

			res := e.svc.RestoreAssignment(context.Background(), "a1", 5)
			if res.Problem != tc.want || res.Assignment != nil {
				t.Fatalf("RestoreAssignment = %+v, want %q", res, tc.want)
			}
			checkProblemLog(t, e, tc.want)

			raw, err := json.Marshal(res)
			if err != nil {
				t.Fatal(err)
			}
			if want := `{"problem":"` + tc.want + `","assignment":null}`; string(raw) != want {
				t.Fatalf("JSON = %s, want %s", raw, want)
			}
		})
	}
}
