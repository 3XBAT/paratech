package agent

import (
	"context"
	"fmt"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Answerer answers a question from the student's lessons (cases/answering, TASK-118): a paid read whose answer is
// shown only in its card (ADR-0022). scope is the reuse scope of its key: the same question is answered once per scope.
// drafts may be nil (no draft is shown yet: TASK-147).
type Answerer interface {
	Ask(ctx context.Context, question string, subject shared.SubjectID, scope string, drafts answering.DraftSink) (answering.Asked, error)
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
	got, err := t.answerer.Ask(ctx, string(q), shared.SubjectID(sub.ID()), t.reuse, nil)
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
