package agent

import (
	"context"
	"fmt"
	"sync"

	"paratech/desktop/internal/cases/agentturn"
)

const opNewReplies = "agent: new reply drafts"

// ReplyDraftCall is the CallID of the reply draft of a request (Draft.Reply): one per request, so the text of a later
// step replaces that of an earlier one (a reply the planner had refused, a preamble before tool calls). The colon is
// never in a tool call's id (values.CheckCallID), so it cannot meet an answer draft.
const ReplyDraftCall = "step:reply"

// Replies is agentturn.Drafts over the panel's Sink (ADR-0026 addendum, TASK-340): the reply a planner step writes goes
// to the panel as a Draft with Reply set, its whole text so far as the one entry of Texts. Like Progress it needs no
// Driver, so app wires it into the step use case without a cycle; the Driver tells it a stored «Стоп» (Cut). The text
// is untrusted model text for display only: never stored, logged or sent to the planner.
type Replies struct {
	sink Sink

	mu   sync.Mutex
	open map[agentturn.TurnRef]*stepDraft // the step of each request whose Hub call runs
}

// NewReplies checks the sink.
func NewReplies(sink Sink) (*Replies, error) {
	if sink == nil {
		return nil, fmt.Errorf("%s: %w: a sink is required", opNewReplies, ErrInvalidConfig)
	}
	return &Replies{sink: sink, open: map[agentturn.TurnRef]*stepDraft{}}, nil
}

// Reply opens the reply draft of step st; the returned ctx is the one of its Hub call, which Cut ends.
func (r *Replies) Reply(ctx context.Context, st agentturn.Step) (context.Context, agentturn.Draft) {
	ctx, cancel := context.WithCancel(ctx)
	ref := refOf(st)
	d := &stepDraft{
		ctx: ctx, cancel: cancel, sink: r.sink, base: Draft{Thread: st.Thread, Turn: st.Turn, CallID: ReplyDraftCall, Reply: true},
	}
	d.forget = func() { r.forget(ref, d) }
	r.mu.Lock()
	old := r.open[ref]
	r.open[ref] = d
	r.mu.Unlock()
	if old != nil {
		old.cut() // the use case runs one step of a request at a time; a left-over one passes nothing more
	}
	return ctx, d
}

// Cut is a stored «Стоп» of ref: the draft of its running step passes nothing more and its Hub call ends at once. The
// Hub finishes and stores the step, so «Продолжить» replays it under the same key.
func (r *Replies) Cut(ref agentturn.TurnRef) {
	r.mu.Lock()
	d := r.open[ref]
	r.mu.Unlock()
	if d != nil {
		d.cut()
	}
}

func (r *Replies) forget(ref agentturn.TurnRef, d *stepDraft) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.open[ref] == d {
		delete(r.open, ref)
	}
}

// stepDraft is the agentturn.Draft of one step. It passes changed texts while its ctx lives and not after its end or a
// cut, so no draft follows the step's update or a «Стоп». Text and end hold mu while they hand a draft to the sink, so
// none is in flight once they returned.
type stepDraft struct {
	ctx    context.Context
	cancel context.CancelFunc
	sink   Sink
	base   Draft
	forget func()

	mu   sync.Mutex
	last string
	sent bool
	done bool
}

// Text passes the reply so far, cleaned like the answer drafts; an unchanged or empty text is not passed.
func (d *stepDraft) Text(soFar string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done || d.ctx.Err() != nil {
		return
	}
	clean := cleanTexts([]string{soFar})
	if len(clean) == 0 || clean[0] == d.last {
		return
	}
	d.last, d.sent = clean[0], true
	out := d.base
	out.Texts = clean
	d.sink.Draft(out)
}

// End closes the draft once the Hub call returned: answered when the step goes on with its reply (the update follows),
// failed when the call failed while its ctx lived, so what the Hub delivered stays. A cut draft (a stored «Стоп»)
// stays dropped, and so does one whose ctx ended.
func (d *stepDraft) End(answered bool) {
	end := DraftFailed
	switch {
	case d.ctx.Err() != nil:
		end = DraftDropped
	case answered:
		end = DraftAnswered
	}
	d.end(end)
	d.cancel()
	d.forget()
}

func (d *stepDraft) cut() {
	d.end(DraftDropped)
	d.cancel()
}

// end tells the sink how the draft ended, once, and only when a draft was passed.
func (d *stepDraft) end(how DraftEnd) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done {
		return
	}
	d.done = true
	if !d.sent {
		return
	}
	out := d.base
	out.End = how
	d.sink.Draft(out)
}
