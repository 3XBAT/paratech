package agentturn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the queue, for error texts.
const (
	opEnqueue = "agentturn: enqueue"
	opQueued  = "agentturn: start queued"
)

// MaxQueued is the most questions that wait in one chat (ADR-0026 addendum 2026-11-08, point 7); one more is busy.
const MaxQueued = 5

// startedKept is how many started tickets a chat remembers, so Withdraw of one that already began says so.
const startedKept = 16

// maxTicket is the longest ticket Validate accepts.
const maxTicket = 32

// ErrNothingQueued means StartQueued found no question to start: none waits, or the chat's request waits for the
// student. Nothing changed.
var ErrNothingQueued = errors.New("agentturn: no question waits to start")

// Ticket names a waiting question («q<N>»), for Withdraw and for the panel to replace its waiting row.
type Ticket string

// Validate checks the form: 1..32 characters of [A-Za-z0-9_-].
func (t Ticket) Validate() error {
	if len(t) == 0 || len(t) > maxTicket {
		return fmt.Errorf("%w: ticket of %d characters", ErrInvalidInput, len(t))
	}
	for _, r := range t {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return fmt.Errorf("%w: ticket %q", ErrInvalidInput, string(t))
		}
	}
	return nil
}

// Queued is a question that waits in chat Thread until the chat's current request is final.
type Queued struct {
	Thread shared.ThreadID
	Ticket Ticket
}

// WithdrawResult is how Withdraw ended.
type WithdrawResult string

// Results of Withdraw.
const (
	WithdrawWithdrawn WithdrawResult = "withdrawn" // it waited and is gone: never sent, never paid
	WithdrawStarted   WithdrawResult = "started"   // it already began as a request: «Стоп» stops it
	WithdrawNotFound  WithdrawResult = "not_found"
)

// waitingAsk is a question that waits, with its own chat (resolved), scope, text, uploads and mentions.
type waitingAsk struct {
	ticket Ticket
	in     AskInput
}

// chatQueue is the queue of one chat: the questions that wait, oldest first, and the tickets that started lately.
type chatQueue struct {
	waiting []waitingAsk
	started []Ticket
}

// queues are the waiting questions of every chat, in memory only: a question still waiting when the app closes is never
// sent. qmu is never held with a call out, and never waits for s.mu, so an Ask queues while a Hub step runs.
type queues struct {
	qmu   sync.Mutex
	next  uint64
	chats map[shared.ThreadID]*chatQueue
}

// chat returns the queue of id, created when absent. qmu is held.
func (q *queues) chat(id shared.ThreadID) *chatQueue {
	if q.chats == nil {
		q.chats = map[shared.ThreadID]*chatQueue{}
	}
	c := q.chats[id]
	if c == nil {
		c = &chatQueue{}
		q.chats[id] = c
	}
	return c
}

// push adds in, its chat set to id, to the queue of chat id unless it is full (thread.ErrTurnInProgress). Unless
// busy (the chat's request runs) the question joins only a queue that already has questions, so they keep their order.
func (q *queues) push(id shared.ThreadID, in AskInput, busy bool) (Queued, bool, error) {
	q.qmu.Lock()
	defer q.qmu.Unlock()
	c := q.chat(id)
	if !busy && len(c.waiting) == 0 {
		return Queued{}, false, nil
	}
	if len(c.waiting) >= MaxQueued {
		return Queued{}, false, fmt.Errorf("%s: %w: %d questions wait", opEnqueue, thread.ErrTurnInProgress, MaxQueued)
	}
	q.next++
	t := Ticket("q" + strconv.FormatUint(q.next, 10))
	in.ThreadID = id
	c.waiting = append(c.waiting, waitingAsk{ticket: t, in: in})
	return Queued{Thread: id, Ticket: t}, true, nil
}

// pop takes the oldest question of chat id and remembers its ticket as started.
func (q *queues) pop(id shared.ThreadID) (waitingAsk, bool) {
	q.qmu.Lock()
	defer q.qmu.Unlock()
	c := q.chats[id]
	if c == nil || len(c.waiting) == 0 {
		return waitingAsk{}, false
	}
	w := c.waiting[0]
	c.waiting = slices.Delete(c.waiting, 0, 1)
	c.started = append(c.started, w.ticket)
	if len(c.started) > startedKept {
		c.started = slices.Delete(c.started, 0, len(c.started)-startedKept)
	}
	return w, true
}

func (q *queues) has(id shared.ThreadID) bool {
	q.qmu.Lock()
	defer q.qmu.Unlock()
	c := q.chats[id]
	return c != nil && len(c.waiting) > 0
}

func (q *queues) withdraw(id shared.ThreadID, t Ticket) WithdrawResult {
	q.qmu.Lock()
	defer q.qmu.Unlock()
	c := q.chats[id]
	if c == nil {
		return WithdrawNotFound
	}
	if i := slices.IndexFunc(c.waiting, func(w waitingAsk) bool { return w.ticket == t }); i >= 0 {
		c.waiting = slices.Delete(c.waiting, i, i+1)
		return WithdrawWithdrawn
	}
	if slices.Contains(c.started, t) {
		return WithdrawStarted
	}
	return WithdrawNotFound
}

// Enqueue queues a question when its chat is busy (ADR-0026 addendum 2026-11-08, point 7): the chat's current request
// is PLANNING or EXECUTING (a Hub step or a tool run alike), or questions already wait there (they keep their order). It
// never waits for s.mu, so it answers while a step runs. A request that waits for the student (AWAITING_PLAN,
// AWAITING_ANSWERS) with nothing queued is not queued: Ask cancels it, as before. ok is false when the question was
// not queued (no chat at all, a chat that does not exist: Ask reports it); a full queue wraps thread.ErrTurnInProgress.
// The input is checked as Ask checks it; mentions that no longer exist are dropped only when it starts.
func (s *Service) Enqueue(ctx context.Context, in AskInput) (Queued, bool, error) {
	if _, err := checkAsk(in); err != nil {
		return Queued{}, false, err
	}
	var th *thread.Thread
	var err error
	if in.ThreadID != "" {
		th, err = s.threads.Get(ctx, in.ThreadID)
	} else {
		th, err = s.threads.Current(ctx)
	}
	if errors.Is(err, shared.ErrNotFound) {
		return Queued{}, false, nil
	}
	if err != nil {
		return Queued{}, false, fmt.Errorf("%s: %w", opEnqueue, err)
	}
	turn, ok := th.Turn()
	return s.q.push(th.ID(), in, ok && running(turn))
}

// HasQueued reports whether a question waits in chat id; it reads memory only.
func (s *Service) HasQueued(id shared.ThreadID) bool {
	return s.q.has(id)
}

// Withdraw is «Отменить этот запрос» of waiting question t of chat id: a question still waiting is removed (never
// sent, never paid); one that already started is WithdrawStarted; any other WithdrawNotFound. It is atomic against
// StartQueued taking it.
func (s *Service) Withdraw(id shared.ThreadID, t Ticket) WithdrawResult {
	return s.q.withdraw(id, t)
}

// StartQueued starts the oldest waiting question of chat id as the next request once the chat's current request is
// final (DONE, FAILED, CANCELLED, STOPPED for any reason) or the chat has none, the same way as Ask with the question's
// own scope, text, uploads and mentions (the ones deleted meanwhile are dropped). A request that waits for the student,
// or no waiting question, is ErrNothingQueued; a request still PLANNING or EXECUTING wraps thread.ErrTurnInProgress
// (the Driver recovers one that no loop drives); a ctx that ended returns its error; none of them changes anything. Every Step of the start carries the
// question's ticket, a failed start too (with its chat and scope), so the panel replaces the waiting row.
func (s *Service) StartQueued(ctx context.Context, id shared.ThreadID) (Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		// The caller went away (the app closes) while it waited for the lock: the question stays unsent.
		return Step{}, fmt.Errorf("%s: %w", opQueued, err)
	}
	th, err := s.threads.Get(ctx, id)
	if err != nil && !errors.Is(err, shared.ErrNotFound) {
		return Step{}, fmt.Errorf("%s: %w", opQueued, err)
	}
	if err == nil {
		if turn, ok := th.Turn(); ok {
			switch {
			case running(turn) && s.q.has(id):
				return Step{}, fmt.Errorf("%s: %w", opQueued, thread.ErrTurnInProgress)
			case running(turn), turn.Status() == thread.StatusAwaitingPlan, turn.Status() == thread.StatusAwaitingAnswers:
				return Step{}, ErrNothingQueued
			}
		}
	}
	// A chat that no longer exists still pops its question: Ask reports it, so the row shows the problem.
	w, ok := s.q.pop(id)
	if !ok {
		return Step{}, ErrNothingQueued
	}
	mentions, err := s.existing(ctx, w.in.Mentions)
	var st Step
	if err == nil {
		st, err = s.ask(ctx, w.in, mentions, w.ticket)
	}
	st.Ticket = w.ticket // abort and conflict build their step from the stored chat
	if err != nil && st.Thread == "" {
		st.Thread, st.Scope = id, w.in.Scope
	}
	return st, err
}

// checkAsk checks the input of an Ask and returns its text: the text and uploads, the chat id, a restart and the
// shape of the mentions.
func checkAsk(in AskInput) (string, error) {
	text, err := values.NewStudentText(in.Text, in.Uploads)
	if err != nil {
		return "", fmt.Errorf("%s: %w: %w", opAsk, ErrInvalidInput, err)
	}
	if in.ThreadID != "" {
		if err := in.ThreadID.Validate(); err != nil {
			return "", fmt.Errorf("%s: %w: %w", opAsk, ErrInvalidInput, err)
		}
	}
	// A restart names its chat: the current chat may have changed since the student chose the message.
	if in.RestartFrom < 0 || (in.RestartFrom > 0 && in.ThreadID == "") {
		return "", fmt.Errorf("%s: %w: restart from request %d of chat %q", opAsk, ErrInvalidInput, in.RestartFrom, in.ThreadID)
	}
	if len(in.Mentions) > values.MaxMentions {
		return "", fmt.Errorf("%s: %w: at most %d mentions", opAsk, ErrInvalidInput, values.MaxMentions)
	}
	for _, m := range in.Mentions {
		if m.IsZero() {
			return "", fmt.Errorf("%s: %w: an empty mention", opAsk, ErrInvalidInput)
		}
	}
	return text, nil
}

// queueBusy queues in after the thread refused it as busy under s.mu (an Ask that raced a request starting): its chat
// is the one asked() loaded, so a question of "the current chat" stays in that chat.
func (s *Service) queueBusy(id shared.ThreadID, in AskInput) (Step, error) {
	q, _, err := s.q.push(id, in, true)
	if err != nil {
		return Step{}, err
	}
	return Step{Thread: q.Thread, Scope: in.Scope, Queued: true, Ticket: q.Ticket}, nil
}
