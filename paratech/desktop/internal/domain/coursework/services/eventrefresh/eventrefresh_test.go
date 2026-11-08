package eventrefresh

import "testing"

// TASK-276: every policy × event or none × due kept, changed or removed.
func TestDecide(t *testing.T) {
	tests := []struct {
		policy                         Policy
		hasEvent, dueChanged, dueClear bool
		want                           Action
	}{
		// Without an event nothing happens, whatever the policy.
		{PolicyAsk, false, true, false, Nothing},
		{PolicyAsk, false, true, true, Nothing},
		{PolicyUpdate, false, true, false, Nothing},
		{PolicyUpdate, false, true, true, Nothing},
		{PolicyKeep, false, true, false, Nothing},
		{PolicyAsk, false, false, false, Nothing},

		// The due did not change: nothing, even with a stray cleared flag.
		{PolicyAsk, true, false, false, Nothing},
		{PolicyUpdate, true, false, false, Nothing},
		{PolicyUpdate, true, false, true, Nothing},
		{PolicyKeep, true, false, false, Nothing},

		// The due changed on an assignment with an event.
		{PolicyAsk, true, true, false, Ask},
		{PolicyAsk, true, true, true, Ask},
		{PolicyUpdate, true, true, false, QueueUpdate},
		{PolicyUpdate, true, true, true, QueueRemoval},
		{PolicyKeep, true, true, false, Nothing},
		{PolicyKeep, true, true, true, Nothing},

		// An unknown policy asks.
		{"", true, true, false, Ask},
		{"always", true, true, true, Ask},
	}
	for _, tt := range tests {
		if got := Decide(tt.policy, tt.hasEvent, tt.dueChanged, tt.dueClear); got != tt.want {
			t.Errorf("Decide(%q, event %v, changed %v, cleared %v) = %q, want %q",
				tt.policy, tt.hasEvent, tt.dueChanged, tt.dueClear, got, tt.want)
		}
	}
}
