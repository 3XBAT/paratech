package agent

import (
	"context"
	"errors"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/shared"
)

const opDrain = "agent: start a waiting question"

// errNothingToStart ends a drain loop that started nothing, without an update.
var errNothingToStart = errors.New("agent: no waiting question started")

// Withdraw is «Отменить этот запрос» of a waiting question (agentturn.Withdraw): it runs at once, no loop.
func (d *Driver) Withdraw(id shared.ThreadID, ticket agentturn.Ticket) agentturn.WithdrawResult {
	return d.turns.Withdraw(id, ticket)
}

func (d *Driver) isClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

// drain gives the waiting questions of thread th their turn: unless the Driver is closed, parent ended or a loop of
// th still runs, a loop starts the oldest one (agentturn.StartQueued; the step use case refuses while the request is not
// final). Every loop calls it once it is no longer among the loops, and an Ask that queued calls it too, so a question
// queued while the last loop of its chat ends is always seen by one of them. A drain loop that started nothing calls it
// only when th was marked deferred meanwhile (a drain met a loop of th), so idle drains of chats that cannot start
// never wake each other. A loop whose thread is not known yet (an Ask of the current chat) may act on th: the drain
// waits for the end of the next loop (later), as that loop may name another chat and then drains only its own.
func (d *Driver) drain(parent context.Context, th shared.ThreadID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.drainLocked(parent, th)
}

// drainLocked is drain with d.mu held.
func (d *Driver) drainLocked(parent context.Context, th shared.ThreadID) {
	if th == "" || parent.Err() != nil || d.closed || !d.turns.HasQueued(th) {
		return
	}
	same, unnamed := d.liveOn(th)
	if same {
		d.deferred[th] = true // that loop drains when it ends, even one that started nothing
		return
	}
	if unnamed {
		d.later[th] = parent
		return
	}
	d.spawn(parent, agentturn.Step{Thread: th}, func(ctx context.Context) (agentturn.Step, error) {
		return d.startQueued(ctx, th)
	})
}

// drainLater runs the drains a loop of no known thread held back; any loop that ends calls it.
func (d *Driver) drainLater() {
	d.mu.Lock()
	defer d.mu.Unlock()
	held := d.later
	d.later = map[shared.ThreadID]context.Context{}
	for th, parent := range held {
		d.drainLocked(parent, th)
	}
}

// startQueued is the operation of a drain loop: the chat is compacted when full, as before an Ask, then its oldest
// waiting question starts. The compaction runs only when a start is ready (agentturn.QueueReady), so a chat whose
// request waits for the student is not summarized on every drain. A request left running with no loop is recovered
// once (unstick, TASK-171), so it cannot hold the queue forever. A start that took no question (nothing waits, the
// request is busy or waits for the student, a failed read) ends the loop with errNothingToStart, so it neither
// publishes nor drains again; a question that started, even with an error, has its ticket on the step and the loop
// goes on as for an Ask.
func (d *Driver) startQueued(ctx context.Context, th shared.ThreadID) (agentturn.Step, error) {
	d.compactReady(ctx, th)
	st, err := d.turns.StartQueued(ctx, th)
	if errors.Is(err, thread.ErrTurnInProgress) && d.unstick(ctx, err, agentturn.IdleInput{Thread: th}) {
		d.compactReady(ctx, th)
		st, err = d.turns.StartQueued(ctx, th)
	}
	if err != nil && st.Ticket == "" {
		if !errors.Is(err, agentturn.ErrNothingQueued) && !errors.Is(err, thread.ErrTurnInProgress) {
			d.log.WarnContext(ctx, opDrain, "thread", th.String(), "err", err)
		}
		return st, errNothingToStart
	}
	return st, err
}

// compactReady compacts chat th when its oldest waiting question can start now.
func (d *Driver) compactReady(ctx context.Context, th shared.ThreadID) {
	if d.compact != nil && d.turns.QueueReady(ctx, th) {
		d.compactBefore(ctx, th)
	}
}

// liveOn reports whether a loop acts on thread th (same) and whether one has not named its thread yet (unnamed). d.mu
// is held.
func (d *Driver) liveOn(th shared.ThreadID) (same, unnamed bool) {
	for _, t := range d.loops {
		same = same || t == th
		unnamed = unnamed || t == ""
	}
	return same, unnamed
}
