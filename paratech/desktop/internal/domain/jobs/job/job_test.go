package job

import "testing"

// The expected list is copied from PRD section 7.2, independently of the constants under test.
func TestStatuses(t *testing.T) {
	want := []string{
		"QUEUED", "RUNNING", "COMPLETED", "RETRY_WAIT", "CANCELLED",
		"FAILED", "WAITING_FOR_NETWORK", "WAITING_FOR_QUOTA", "WAITING_FOR_POWER",
	}
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

func TestStatusesReturnsFreshSlice(t *testing.T) {
	a := Statuses()
	a[0] = "MUTATED"
	if Statuses()[0] != StatusQueued {
		t.Fatal("Statuses() shares state between calls")
	}
}
