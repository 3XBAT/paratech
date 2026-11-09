package wails

import (
	"context"
	"encoding/json"
	"testing"

	"paratech/desktop/internal/cases/agentturn"
)

// TASK-398: a question asked while the chat's request runs comes back queued with its ticket; one that started has
// neither; a refusal neither.
func TestAgentAskQueued(t *testing.T) {
	s, d, _, _ := newAgent(t, readyLink())
	d.queued = agentturn.Queued{Thread: "th1", Ticket: "q7"}
	res := s.Ask(context.Background(), AgentAskInput{Text: "а ещё"})
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"problem":"","queued":true,"ticket":"q7"}`; string(b) != want {
		t.Fatalf("Ask = %s, want %s", b, want)
	}

	d.queued = agentturn.Queued{}
	if res := s.Ask(context.Background(), AgentAskInput{Text: "сразу"}); res != (AgentAskResult{}) {
		t.Fatalf("started Ask = %+v", res)
	}
	if res := s.Ask(context.Background(), AgentAskInput{Text: "x", ThreadID: "a/b"}); res != (AgentAskResult{Problem: AgentProblemInvalid}) {
		t.Fatalf("invalid Ask = %+v", res)
	}
}

// Withdraw passes a valid chat and ticket to the Driver and maps its answer; a malformed one is refused before it.
func TestAgentWithdraw(t *testing.T) {
	tests := []struct {
		name, thread, ticket string
		driver               agentturn.WithdrawResult
		want                 string
		called               bool
	}{
		{"withdrawn", "th1", "q7", agentturn.WithdrawWithdrawn, AgentWithdrawn, true},
		{"started", "th1", "q7", agentturn.WithdrawStarted, AgentWithdrawStarted, true},
		{"not found", "th1", "q7", agentturn.WithdrawNotFound, AgentWithdrawMissing, true},
		{"unknown answer", "th1", "q7", "odd", AgentWithdrawFailed, true},
		{"bad chat", "a/b", "q7", agentturn.WithdrawWithdrawn, AgentWithdrawInvalid, false},
		{"empty chat", "", "q7", agentturn.WithdrawWithdrawn, AgentWithdrawInvalid, false},
		{"bad ticket", "th1", "q 7", agentturn.WithdrawWithdrawn, AgentWithdrawInvalid, false},
		{"empty ticket", "th1", "", agentturn.WithdrawWithdrawn, AgentWithdrawInvalid, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, d, _, _ := newAgent(t, readyLink())
			d.withdraw = tt.driver
			res := s.Withdraw(context.Background(), tt.thread, tt.ticket)
			b, err := json.Marshal(res)
			if err != nil {
				t.Fatal(err)
			}
			if want := `{"result":"` + tt.want + `"}`; string(b) != want {
				t.Fatalf("Withdraw = %s, want %s", b, want)
			}
			if called := len(d.withdrawn) == 1; called != tt.called {
				t.Fatalf("driver called %v, want %v", d.withdrawn, tt.called)
			}
			if tt.called && d.withdrawn[0] != (agentturn.Queued{Thread: "th1", Ticket: "q7"}) {
				t.Fatalf("driver got %+v", d.withdrawn[0])
			}
		})
	}
}

// The update of a question that waits, and the first update of its start, carry the ticket (always sent).
func TestAgentPanelQueuedContract(t *testing.T) {
	panel := NewAgentPanel(discard())
	em := &fakeEmitter{}
	panel.Attach(em)
	panel.Publish(AgentUpdate{Thread: "th1", Queued: true, Ticket: "q7"})
	panel.Publish(AgentUpdate{Thread: "th1", Turn: 2, Status: "PLANNING", Progress: true, Ticket: "q7"})
	if len(em.events) != 2 {
		t.Fatalf("events = %+v", em.events)
	}
	wants := []string{
		`{"threadId":"th1","turn":0,"subjectId":"","lessonId":"","assignmentId":"","screen":"",` +
			`"status":"","stopReason":"","problem":"","progress":false,"retry":0,"ticket":"q7","queued":true,"cards":[],"fill":null,"compaction":false,"compacting":false}`,
		`{"threadId":"th1","turn":2,"subjectId":"","lessonId":"","assignmentId":"","screen":"",` +
			`"status":"PLANNING","stopReason":"","problem":"","progress":true,"retry":0,"ticket":"q7","queued":false,"cards":[],"fill":null,"compaction":false,"compacting":false}`,
	}
	for i, want := range wants {
		got, err := json.Marshal(em.events[i].data[0])
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("event %d JSON =\n%s\nwant\n%s", i, got, want)
		}
	}
}
