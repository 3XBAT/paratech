package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentchats"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/shared"
)

// fakeFills answers Fill with what it is set to and records the chats asked.
type fakeFills struct {
	mu    sync.Mutex
	fill  agentchats.Fill
	err   error
	asked []shared.ThreadID
	ctxOK bool // the last ctx had a deadline and was not cancelled
}

func (f *fakeFills) Fill(ctx context.Context, id shared.ThreadID) (agentchats.Fill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	_, deadline := ctx.Deadline()
	f.ctxOK = deadline && ctx.Err() == nil
	return f.fill, f.err
}

// TestAgentUpdateFillAtTheEndOfARequest: the update that ends a request carries the ring of its chat; an update during
// a step, a waiting card or a failed operation carries none and reads nothing.
func TestAgentUpdateFillAtTheEndOfARequest(t *testing.T) {
	high := agentchats.Fill{Percent: 72, Level: thread.FillHigh}
	tests := []struct {
		name string
		u    AgentUpdate
		want bool
	}{
		{"done", AgentUpdate{Thread: "th1", Turn: 2, Status: thread.StatusDone}, true},
		{"stopped", AgentUpdate{Thread: "th1", Turn: 2, Status: thread.StatusStopped}, true},
		{"failed", AgentUpdate{Thread: "th1", Turn: 2, Status: thread.StatusFailed}, true},
		{"cancelled", AgentUpdate{Thread: "th1", Turn: 2, Status: thread.StatusCancelled}, true},
		{"done with a problem", AgentUpdate{Thread: "th1", Turn: 2, Status: thread.StatusDone, Problem: "stale"}, true},
		{"planning", AgentUpdate{Thread: "th1", Turn: 2, Status: thread.StatusPlanning}, false},
		{"executing", AgentUpdate{Thread: "th1", Turn: 2, Status: thread.StatusExecuting}, false},
		{"awaiting plan", AgentUpdate{Thread: "th1", Turn: 2, Status: thread.StatusAwaitingPlan}, false},
		{"awaiting answers", AgentUpdate{Thread: "th1", Turn: 2, Status: thread.StatusAwaitingAnswers}, false},
		{"progress", AgentUpdate{Thread: "th1", Turn: 2, Status: thread.StatusDone, Progress: true}, false},
		{"failed operation", AgentUpdate{Thread: "th1", Turn: 2, Problem: "busy"}, false},
		{"no thread", AgentUpdate{Status: thread.StatusFailed, Problem: "failed"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fills := &fakeFills{fill: high}
			panel := NewAgentPanel(discard())
			panel.SetFills(fills)
			em := &fakeEmitter{}
			panel.Attach(em)
			panel.Publish(tt.u)

			v := em.events[0].data[0].(AgentUpdateView)
			if tt.want {
				if v.Fill == nil || *v.Fill != (AgentFillView{Percent: 72, Level: "high"}) {
					t.Fatalf("fill = %+v, want the chat's", v.Fill)
				}
				if len(fills.asked) != 1 || fills.asked[0] != "th1" || !fills.ctxOK {
					t.Fatalf("asked %v (ctx with a deadline %v)", fills.asked, fills.ctxOK)
				}
				return
			}
			if v.Fill != nil || len(fills.asked) != 0 {
				t.Fatalf("fill = %+v, asked %v; want none", v.Fill, fills.asked)
			}
		})
	}
}

// A fill that cannot be read is logged and the update still goes out, with a null fill; without fills nothing is read.
func TestAgentUpdateFillFailure(t *testing.T) {
	var logs bytes.Buffer
	panel := NewAgentPanel(slog.New(slog.NewTextHandler(&logs, nil)))
	panel.SetFills(&fakeFills{err: errors.New("db down")})
	em := &fakeEmitter{}
	panel.Attach(em)
	panel.Publish(AgentUpdate{Thread: "th1", Turn: 1, Status: thread.StatusDone})
	if len(em.events) != 1 || em.events[0].data[0].(AgentUpdateView).Fill != nil {
		t.Fatalf("events = %+v", em.events)
	}
	if !strings.Contains(logs.String(), opAgentFill) || !strings.Contains(logs.String(), "db down") {
		t.Fatalf("log = %s", logs.String())
	}

	panel.SetFills(nil)
	panel.Publish(AgentUpdate{Thread: "th1", Turn: 1, Status: thread.StatusDone})
	if len(em.events) != 2 || em.events[1].data[0].(AgentUpdateView).Fill != nil {
		t.Fatalf("events = %+v", em.events)
	}
}

// The JSON of the update that ends a request: fill is {percent, level, overflow}; an overflow is full.
func TestAgentUpdateFillContract(t *testing.T) {
	panel := NewAgentPanel(discard())
	panel.SetFills(&fakeFills{fill: agentchats.Fill{Percent: 100, Level: thread.FillFull, Overflow: true}})
	em := &fakeEmitter{}
	panel.Attach(em)
	panel.Publish(AgentUpdate{Thread: "th1", Turn: 4, Status: thread.StatusDone})
	got, err := json.Marshal(em.events[0].data[0])
	if err != nil {
		t.Fatal(err)
	}
	want := `{"threadId":"th1","turn":4,"subjectId":"","lessonId":"","assignmentId":"","screen":"",` +
		`"status":"DONE","stopReason":"","problem":"","progress":false,"cards":[],` +
		`"fill":{"percent":100,"level":"full","overflow":true}}`
	if string(got) != want {
		t.Errorf("event JSON =\n%s\nwant\n%s", got, want)
	}
}

// ChatCurrent of a stopped request carries its chat's ring as the update that stopped it did; a waiting one none.
func TestAgentChatCurrentFill(t *testing.T) {
	c := &fakeCurrent{ok: true, update: AgentUpdate{Thread: "th1", Turn: 2, Status: thread.StatusStopped}}
	s, _ := newCurrentAgent(t, c)
	s.panel.SetFills(&fakeFills{fill: agentchats.Fill{Percent: 35, Level: thread.FillNormal}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if v := s.ChatCurrent(ctx, "th1"); v.Update == nil || v.Update.Fill == nil || v.Update.Fill.Percent != 35 ||
		v.Update.Fill.Level != "normal" {
		t.Fatalf("ChatCurrent = %+v", v)
	}
	c.update.Status = thread.StatusAwaitingPlan
	if v := s.ChatCurrent(ctx, "th1"); v.Update == nil || v.Update.Fill != nil {
		t.Fatalf("ChatCurrent of a waiting request = %+v", v)
	}
}
