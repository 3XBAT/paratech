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

func TestStatusClassification(t *testing.T) {
	cases := []struct {
		status   Status
		terminal bool
	}{
		{StatusQueued, false}, {StatusRunning, false}, {StatusRetryWait, false},
		{StatusWaitingForNetwork, false}, {StatusWaitingForQuota, false}, {StatusWaitingForPower, false},
		{StatusCompleted, true}, {StatusFailed, true}, {StatusCancelled, true},
		{Status("BOGUS"), false},
	}
	for _, tc := range cases {
		t.Run(tc.status.String(), func(t *testing.T) {
			if got := tc.status.IsTerminal(); got != tc.terminal {
				t.Fatalf("IsTerminal() = %v, want %v", got, tc.terminal)
			}
		})
	}
}

// The expected list is copied from PRD section 7.2: every status that is not a final state of the lifecycle.
func TestActiveStatuses(t *testing.T) {
	want := []Status{
		StatusQueued, StatusRunning, StatusRetryWait,
		StatusWaitingForNetwork, StatusWaitingForQuota, StatusWaitingForPower,
	}
	got := ActiveStatuses()
	if len(got) != len(want) {
		t.Fatalf("ActiveStatuses() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ActiveStatuses()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
