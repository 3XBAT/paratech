package recording

import "testing"

// The expected list is copied from PRD section 6.2, independently of the constants under test.
func TestStatuses(t *testing.T) {
	want := []string{"STARTING", "RECORDING", "STOPPING", "FINALIZING", "COMPLETED", "FAILED", "RECOVERY_REQUIRED"}
	got := Statuses()
	if len(got) != len(want) {
		t.Fatalf("Statuses() has %d values, want %d", len(got), len(want))
	}
	seen := map[Status]bool{}
	for i, s := range got {
		if s.String() != want[i] {
			t.Errorf("Statuses()[%d] = %q, want %q", i, s, want[i])
		}
		if seen[s] {
			t.Errorf("duplicate status %q", s)
		}
		seen[s] = true
	}
}
