package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Answerer answers a question from the student's lessons (cases/answering, TASK-118): a paid read whose answer is
// shown only in its card (ADR-0022). scope is the reuse scope of its key: the same question is answered once per scope.
// drafts gets the draft of the answer while it is read.
type Answerer interface {
	Ask(ctx context.Context, question string, subject shared.SubjectID, scope string, drafts answering.DraftSink) (answering.Asked, error)
}

// Draft is the draft of the answer an answer_question call (CallID) of a request is reading (ADR-0026): the claim
// texts written so far, untrusted model text for display as «черновик» only, never stored, logged or sent to the
// planner. Texts are cleaned of format characters (bidi controls and the like) and never empty: the drafts of a call
// are its texts, then one Draft with End set and no texts.
type Draft struct {
	Thread shared.ThreadID
	Turn   int
	CallID string
	Texts  []string
	End    DraftEnd
}

// DraftEnd says how the read of a call ended; the zero value marks a draft of a read still running.
type DraftEnd string

// Ends of a read.
const (
	DraftReading  DraftEnd = ""
	DraftAnswered DraftEnd = "answered" // an answer was made: its card follows, the last draft may be shown till then
	DraftDropped  DraftEnd = "dropped"  // no answer (failed, cut by «Стоп» or the ctx): the draft goes, nothing replaces it
)

// callDraft is the DraftSink of one answer_question call: it passes the changed drafts of the read while its ctx lives
// and not after end, so no draft follows the outcome, a «Стоп» or a cut read. Draft and end hold mu while they hand a
// draft to the sink, so none is in flight once end returned.
type callDraft struct {
	ctx  context.Context
	sink Sink
	base Draft // the request and the call

	mu   sync.Mutex
	last []string
	sent bool
	done bool
}

// Draft passes texts cleaned; an unchanged or empty draft is not passed.
func (c *callDraft) Draft(texts []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done || c.ctx.Err() != nil {
		return
	}
	clean := cleanTexts(texts)
	if len(clean) == 0 || slices.Equal(clean, c.last) {
		return
	}
	c.last, c.sent = clean, true
	d := c.base
	d.Texts = slices.Clone(clean)
	c.sink.Draft(d)
}

// end closes the call's drafts; the sink hears of it only when a draft was passed. A cut ctx drops the draft even when
// an answer came: the Driver shows no outcome of a cut read.
func (c *callDraft) end(answered bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	c.done = true
	if !c.sent {
		return
	}
	d := c.base
	d.End = DraftDropped
	if answered && c.ctx.Err() == nil {
		d.End = DraftAnswered
	}
	c.sink.Draft(d)
}

// cleanTexts drops the format characters (Unicode Cf: bidi controls, zero-width marks) of the texts, which could
// reorder or hide what the draft shows, and the texts left empty.
func cleanTexts(texts []string) []string {
	out := make([]string, 0, len(texts))
	for _, s := range texts {
		s = strings.Map(func(r rune) rune {
			if unicode.Is(unicode.Cf, r) {
				return -1
			}
			return r
		}, s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// answerScope is the reuse scope of the answers of step st: its request (thread and turn). «Продолжить» replays the
// paid answer of the request; «Повторить» is another turn, so a fresh answer (ADR-0026).
func answerScope(st agentturn.Step) string { return fmt.Sprintf("ag:%s:%d", st.Thread, st.Turn) }

// answerCards is the card of an answer_question that made an answer: the panel reads its text by ID from the core
// (AnswerService), so neither the planner nor the step carries it.
func answerCards(c agentturn.Call, out agentturn.Outcome) []values.Card {
	if c.Call.Tool() != values.ToolAnswerQuestion || out.Result.IsError {
		return nil
	}
	var cards []values.Card
	for _, r := range out.Seen {
		if r.Kind() == values.KindAnswer {
			cards = append(cards, values.AnswerCard{Answer: r})
		}
	}
	return cards
}

// answerQuestion asks the reader ANSWER about the lessons of the subject in scope, or of the whole base for scope
// "all". The planner gets the answer to show as a card and its counts; never the question echoed back, the claims, the
// quotes or a lesson's text.
func (t tools) answerQuestion(ctx context.Context, call values.Call, scope values.Scope) (values.ToolResult, []values.Ref, error) {
	q, _ := call.Args().Query("question")
	sub := values.SearchSubjectOf(call.Args(), scope)
	var drafts answering.DraftSink
	var bound *callDraft
	if t.sink != nil {
		bound = &callDraft{ctx: ctx, sink: t.sink, base: Draft{Thread: t.turn.Thread, Turn: t.turn.Turn, CallID: call.ID()}}
		drafts = bound
	}
	got, err := t.answerer.Ask(ctx, string(q), shared.SubjectID(sub.ID()), t.reuse, drafts)
	if bound != nil {
		bound.end(err == nil && got.Answer != nil)
	}
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	a := got.Answer
	if a == nil {
		return values.AnswerResult(call.ID(), values.AnswerRow{})
	}
	ref, err := values.NewRef(values.KindAnswer, string(a.ID()))
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	c := a.Counts()
	return values.AnswerResult(call.ID(), values.AnswerRow{
		Answer: ref, Fragments: c.Fragments, Claims: c.Claims, Supported: c.Supported, Unsupported: c.Unsupported,
		Missing: c.Missing, Sources: c.Sources,
	})
}
