package wails

import (
	"slices"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/shared"
)

// AgentDraftEvent is the Wails event of the draft of an answer being read (ADR-0026); its data is one AgentDraftView.
const AgentDraftEvent = "agent:draft"

// agentDraftEvery bounds the drafts of one call to 10 a second; the last texts win.
const agentDraftEvery = 100 * time.Millisecond

// AgentDraftEnd says how the read of a call ended; "" while it runs.
type AgentDraftEnd string

// Ends of a read, as the Driver names them.
const (
	AgentDraftReading  AgentDraftEnd = ""
	AgentDraftAnswered AgentDraftEnd = "answered" // the answer card follows: the last texts are shown before it
	AgentDraftDropped  AgentDraftEnd = "dropped"  // no answer: the texts not shown yet are dropped
)

// AgentDraft is a draft as the Driver hands it; app maps the Driver's own type to it. Texts are the claim texts
// written so far (cleaned by the Driver); a draft with End set has none.
type AgentDraft struct {
	Thread shared.ThreadID
	Turn   int
	CallID string
	Texts  []string
	End    AgentDraftEnd
}

// AgentDraftView is the JSON form of a draft: the request, the answer_question call and its texts so far. The texts
// are untrusted model text, unchecked: the panel renders them as plain text marked «черновик» until the update with
// the answer card of the call (or the stopped request) replaces them.
type AgentDraftView struct {
	ThreadID string   `json:"threadId"`
	Turn     int      `json:"turn"`
	CallID   string   `json:"callId"`
	Texts    []string `json:"texts"`
}

// draftClock gives the time and the timers of the throttle; tests use a fake.
type draftClock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

func (wallClock) AfterFunc(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }

// draftTurn is the drafts of the latest request (turn) of a thread; one per thread is kept. cut is set by an update of
// the request that is no longer running (a «Стоп», a cut or ended request): its drafts are dropped until it runs
// again. The «Стоп» update comes from another operation than the read, so a draft may race it; the Driver itself hands
// no draft of a call after its end, which comes before the update with the call's outcome.
type draftTurn struct {
	turn  int
	cut   bool
	calls map[string]*callDraft
}

// callDraft is the throttle of one call: texts wait for the timer when a draft was emitted less than agentDraftEvery
// ago.
type callDraft struct {
	texts []string    // not emitted yet; nil none
	at    time.Time   // when a draft was last emitted; zero none
	stop  func() bool // the timer of texts; nil none
}

// Draft emits the draft d at most every agentDraftEvery per call, the last texts winning. An answered end emits the
// texts still waiting at once (the card follows), a dropped end drops them; no draft of a cut request is emitted.
func (p *AgentPanel) Draft(d AgentDraft) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.draftTurn(d.Thread, d.Turn)
	if t == nil {
		return
	}
	c := t.calls[d.CallID]
	if d.End != AgentDraftReading {
		if c != nil {
			if d.End == AgentDraftAnswered && !t.cut {
				p.flushDraft(d.Thread, t.turn, d.CallID, c)
			}
			c.halt()
			delete(t.calls, d.CallID)
		}
		return
	}
	if t.cut || len(d.Texts) == 0 {
		return
	}
	if c == nil {
		c = &callDraft{}
		t.calls[d.CallID] = c
	}
	c.texts = slices.Clone(d.Texts)
	if c.stop != nil {
		return // the timer emits the last texts
	}
	now := p.clock.Now()
	if wait := c.at.Add(agentDraftEvery).Sub(now); !c.at.IsZero() && wait > 0 {
		th, turn, id := d.Thread, t.turn, d.CallID
		c.stop = p.clock.AfterFunc(wait, func() { p.draftDue(th, turn, id, c) })
		return
	}
	p.emitDraft(d.Thread, t.turn, d.CallID, c, now)
}

// draftDue emits the texts of a call whose wait is over, unless the call ended or its request was cut meanwhile.
func (p *AgentPanel) draftDue(th shared.ThreadID, turn int, id string, c *callDraft) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.drafts[th]
	if t == nil || t.turn != turn || t.cut || t.calls[id] != c {
		return
	}
	c.stop = nil
	p.emitDraft(th, turn, id, c, p.clock.Now())
}

// draftsBefore settles the drafts of the request of u before u is emitted: an update of a running request (its calls
// ran) emits the texts still waiting, so the last draft comes before the card; an update of a request that stopped
// or ended drops them and cuts the drafts that come late. Progress updates and refused operations (a problem: another
// operation of the thread was busy or stale) leave the drafts. p.mu is held.
func (p *AgentPanel) draftsBefore(u AgentUpdate) {
	if u.Progress || u.Problem != "" || u.Thread == "" {
		return
	}
	t := p.draftTurn(u.Thread, u.Turn)
	if t == nil {
		return
	}
	running := u.Status == thread.StatusPlanning || u.Status == thread.StatusExecuting
	for id, c := range t.calls {
		if running {
			p.flushDraft(u.Thread, t.turn, id, c)
		}
		c.halt()
		delete(t.calls, id)
	}
	t.cut = !running
}

// draftTurn returns the drafts of request turn of thread th, replacing those of an older request; nil for a request
// older than the one known. p.mu is held.
func (p *AgentPanel) draftTurn(th shared.ThreadID, turn int) *draftTurn {
	t := p.drafts[th]
	switch {
	case t != nil && turn < t.turn:
		return nil
	case t == nil || turn > t.turn:
		if t != nil {
			for _, c := range t.calls {
				c.halt()
			}
		}
		t = &draftTurn{turn: turn, calls: map[string]*callDraft{}}
		p.drafts[th] = t
	}
	return t
}

// flushDraft emits the texts of c still waiting. p.mu is held.
func (p *AgentPanel) flushDraft(th shared.ThreadID, turn int, id string, c *callDraft) {
	c.halt()
	p.emitDraft(th, turn, id, c, p.clock.Now())
}

// emitDraft emits the texts of c, if any wait. p.mu is held.
func (p *AgentPanel) emitDraft(th shared.ThreadID, turn int, id string, c *callDraft, now time.Time) {
	if c.texts == nil {
		return
	}
	v := AgentDraftView{ThreadID: string(th), Turn: turn, CallID: id, Texts: c.texts}
	c.texts, c.at = nil, now
	if p.emit != nil {
		p.emit.Emit(AgentDraftEvent, v)
	}
}

// halt stops the timer of c.
func (c *callDraft) halt() {
	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
}
