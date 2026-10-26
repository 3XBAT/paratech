package wails

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/values"
)

// fakeDraftClock is a clock the test moves; its timers fire in advance, on the test goroutine.
type fakeDraftClock struct {
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Time
	f       func()
	stopped bool
	fired   bool
}

func (c *fakeDraftClock) Now() time.Time { return c.now }

func (c *fakeDraftClock) AfterFunc(d time.Duration, f func()) func() bool {
	t := &fakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return func() bool {
		live := !t.stopped && !t.fired
		t.stopped = true
		return live
	}
}

func (c *fakeDraftClock) advance(d time.Duration) {
	c.now = c.now.Add(d)
	for _, t := range slices.Clone(c.timers) {
		if !t.stopped && !t.fired && !t.at.After(c.now) {
			t.fired = true
			t.f()
		}
	}
}

func draftPanel(t *testing.T) (*AgentPanel, *fakeEmitter, *fakeDraftClock) {
	t.Helper()
	clock := &fakeDraftClock{now: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)}
	p := NewAgentPanel(discard())
	p.clock = clock
	em := &fakeEmitter{}
	p.Attach(em)
	return p, em, clock
}

// shown is what the page got: "update" for an update, the texts joined for a draft.
func shown(t *testing.T, em *fakeEmitter) []string {
	t.Helper()
	em.mu.Lock()
	defer em.mu.Unlock()
	var out []string
	for _, e := range em.events {
		switch v := e.data[0].(type) {
		case AgentDraftView:
			if e.name != AgentDraftEvent {
				t.Fatalf("draft under %q", e.name)
			}
			s := ""
			for _, x := range v.Texts {
				s += x + "|"
			}
			out = append(out, s)
		case AgentUpdateView:
			out = append(out, "update")
		default:
			t.Fatalf("event %q with %T", e.name, v)
		}
	}
	return out
}

func reading(callID string, texts ...string) AgentDraft {
	return AgentDraft{Thread: "th1", Turn: 1, CallID: callID, Texts: texts}
}

func ended(callID string, end AgentDraftEnd) AgentDraft {
	return AgentDraft{Thread: "th1", Turn: 1, CallID: callID, End: end}
}

// The JSON contract of the draft event the panel (TASK-148) reads: strings only.
func TestAgentDraftEventContract(t *testing.T) {
	p, em, _ := draftPanel(t)
	p.Draft(AgentDraft{Thread: "th1", Turn: 2, CallID: "c1", Texts: []string{"Ряд сходится", "второе"}})
	if len(em.events) != 1 || em.events[0].name != "agent:draft" || len(em.events[0].data) != 1 {
		t.Fatalf("events = %+v", em.events)
	}
	got, err := json.Marshal(em.events[0].data[0])
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"threadId":"th1","turn":2,"callId":"c1","texts":["Ряд сходится","второе"]}`
	if string(got) != want {
		t.Fatalf("payload = %s\nwant      %s", got, want)
	}
}

// At most one draft per call every 100 ms, the last texts winning; calls are throttled apart.
func TestAgentDraftThrottle(t *testing.T) {
	p, em, clock := draftPanel(t)
	p.Draft(reading("c1", "а"))
	p.Draft(reading("c1", "аб"))
	p.Draft(reading("c1", "абв"))
	p.Draft(reading("c2", "x")) // another call: its own pace
	clock.advance(50 * time.Millisecond)
	if got := shown(t, em); !slices.Equal(got, []string{"а|", "x|"}) {
		t.Fatalf("before 100 ms: %q", got)
	}
	clock.advance(50 * time.Millisecond)
	if got := shown(t, em); !slices.Equal(got, []string{"а|", "x|", "абв|"}) {
		t.Fatalf("at 100 ms: %q", got)
	}
	clock.advance(time.Second) // nothing waits: no event
	p.Draft(reading("c1", "абвг"))
	if got := shown(t, em); !slices.Equal(got, []string{"а|", "x|", "абв|", "абвг|"}) {
		t.Fatalf("after a pause: %q", got)
	}

	// A draft every millisecond for a second: one event at once, then one per 100 ms.
	p, em, clock = draftPanel(t)
	for i := range 1000 {
		p.Draft(reading("c1", string(rune('a'+i%26))))
		clock.advance(time.Millisecond)
	}
	if n := len(shown(t, em)); n != 11 {
		t.Fatalf("%d events in a second, want 11", n)
	}
}

// The texts still waiting are emitted before the update that shows the call's card, whether the answered end or the
// update comes first.
func TestAgentDraftFlushedBeforeTheCard(t *testing.T) {
	card := AgentUpdate{Thread: "th1", Turn: 1, Status: "EXECUTING",
		Cards: []values.Card{values.AnswerCard{Answer: mustRef(t, values.KindAnswer, "an1")}}}

	p, em, clock := draftPanel(t)
	p.Draft(reading("c1", "а"))
	p.Draft(reading("c1", "аб"))
	p.Draft(ended("c1", AgentDraftAnswered))
	p.Publish(card)
	clock.advance(time.Second)
	if got := shown(t, em); !slices.Equal(got, []string{"а|", "аб|", "update"}) {
		t.Fatalf("answered end, then card: %q", got)
	}

	p, em, clock = draftPanel(t)
	p.Draft(reading("c1", "а"))
	p.Draft(reading("c1", "аб"))
	p.Publish(card)
	clock.advance(time.Second) // the timer of "аб" was stopped: no second "аб"
	if got := shown(t, em); !slices.Equal(got, []string{"а|", "аб|", "update"}) {
		t.Fatalf("card without an end: %q", got)
	}
}

// After «Стоп» (the stopped request's update) or a cut read no further draft of the call is emitted, not even the
// texts that waited; a progress update or a refused operation leaves the drafts.
func TestAgentDraftStopAndCut(t *testing.T) {
	stopped := AgentUpdate{Thread: "th1", Turn: 1, Status: "STOPPED", StopReason: values.StopUser,
		Cards: []values.Card{values.StoppedCard{Reason: values.StopUser}}}
	tests := []struct {
		name string
		cut  func(p *AgentPanel)
		want []string
	}{
		{"stop", func(p *AgentPanel) { p.Publish(stopped) }, []string{"а|", "update"}},
		{"dropped end", func(p *AgentPanel) { p.Draft(ended("c1", AgentDraftDropped)) }, []string{"а|"}},
		{"failed request", func(p *AgentPanel) { p.Publish(AgentUpdate{Thread: "th1", Turn: 1, Status: "FAILED"}) },
			[]string{"а|", "update"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, em, clock := draftPanel(t)
			p.Draft(reading("c1", "а"))
			p.Draft(reading("c1", "аб")) // waits for the timer
			tt.cut(p)
			clock.advance(time.Second)
			if tt.name != "dropped end" {
				p.Draft(reading("c1", "абв")) // late, after the cut
				p.Draft(reading("c2", "x"))   // another call of the cut request
				p.Draft(ended("c1", AgentDraftAnswered))
			}
			clock.advance(time.Second)
			if got := shown(t, em); !slices.Equal(got, tt.want) {
				t.Fatalf("shown %q, want %q", got, tt.want)
			}
		})
	}

	// «Продолжить» runs the request again: its drafts show again.
	p, em, clock := draftPanel(t)
	p.Publish(stopped)
	p.Draft(reading("c1", "а"))
	p.Publish(AgentUpdate{Thread: "th1", Turn: 1, Status: "EXECUTING"})
	p.Draft(reading("c1", "б"))
	clock.advance(time.Second)
	if got := shown(t, em); !slices.Equal(got, []string{"update", "update", "б|"}) {
		t.Fatalf("after «Продолжить»: %q", got)
	}

	// A progress update and a refused operation of the thread (busy) neither flush nor cut.
	p, em, clock = draftPanel(t)
	p.Draft(reading("c1", "а"))
	p.Draft(reading("c1", "аб"))
	p.Publish(AgentUpdate{Thread: "th1", Turn: 1, Status: "PLANNING", Progress: true})
	p.Publish(AgentUpdate{Thread: "th1", Turn: 1, Problem: AgentProblemBusy})
	clock.advance(time.Second)
	if got := shown(t, em); !slices.Equal(got, []string{"а|", "update", "update", "аб|"}) {
		t.Fatalf("progress and busy: %q", got)
	}

	// A newer request of the thread drops the drafts of the older one.
	p, em, clock = draftPanel(t)
	p.Draft(reading("c1", "а"))
	p.Draft(reading("c1", "аб"))
	p.Publish(AgentUpdate{Thread: "th1", Turn: 2, Status: "PLANNING"})
	p.Draft(reading("c1", "старый"))
	clock.advance(time.Second)
	if got := shown(t, em); !slices.Equal(got, []string{"а|", "update"}) {
		t.Fatalf("newer request: %q", got)
	}
}

// Without a page nothing is emitted, and the texts of ended calls are forgotten.
func TestAgentDraftDetachedAndForgotten(t *testing.T) {
	p, em, clock := draftPanel(t)
	p.Attach(nil)
	p.Draft(reading("c1", "а"))
	p.Draft(reading("c1", "аб"))
	p.Draft(ended("c1", AgentDraftAnswered))
	clock.advance(time.Second)
	if len(em.events) != 0 {
		t.Fatalf("events without a page: %+v", em.events)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if th := p.drafts["th1"]; th == nil || len(th.calls) != 0 {
		t.Fatalf("drafts kept: %+v", th)
	}
}
