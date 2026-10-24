package agent

import (
	"context"

	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Answerer answers a question from the student's lessons (cases/answering, TASK-118): a paid read whose answer is
// shown only in its card (ADR-0022).
type Answerer interface {
	Ask(ctx context.Context, question string, subject shared.SubjectID) (answering.Asked, error)
}

// answerQuestion asks the reader ANSWER about the lessons of the subject in scope, or of the whole base for scope
// "all". The planner gets the answer to show as a card and its counts; never the question echoed back, the claims, the
// quotes or a lesson's text.
func (t tools) answerQuestion(ctx context.Context, call values.Call, scope values.Scope) (values.ToolResult, []values.Ref, error) {
	q, _ := call.Args().Query("question")
	sub := values.SearchSubjectOf(call.Args(), scope)
	got, err := t.answerer.Ask(ctx, string(q), shared.SubjectID(sub.ID()))
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
