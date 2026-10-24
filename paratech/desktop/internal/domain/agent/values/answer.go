package values

import "fmt"

// AnswerRow is the result of answer_question (ADR-0022): the answer to open in its card and numbers only, never the
// question, the claims, the quotes or the lessons' text. Answer is zero when no fragment of the lessons matched the
// question (Fragments 0): nothing was asked.
type AnswerRow struct {
	Answer      Ref
	Fragments   int // fragments of the lessons sent to the reader
	Claims      int
	Supported   int // claims with a citation that passed the check
	Unsupported int
	Missing     int // parts of the question the lessons do not answer
	Sources     int // places in the lessons the citations open
}

// AnswerResult is the result of answer_question and the IDs it shows.
func AnswerResult(callID string, r AnswerRow) (ToolResult, []Ref, error) {
	if err := wantRef(r.Answer, KindAnswer, false); err != nil {
		return ToolResult{}, nil, err
	}
	switch {
	case r.Fragments < 0 || r.Claims < 0 || r.Supported < 0 || r.Unsupported < 0 || r.Missing < 0 || r.Sources < 0 ||
		r.Supported+r.Unsupported != r.Claims:
		return ToolResult{}, nil, fmt.Errorf("%w: answer counts", ErrInvalidResult)
	case r.Answer.IsZero() != (r.Fragments == 0):
		return ToolResult{}, nil, fmt.Errorf("%w: an answer is made exactly when fragments were sent", ErrInvalidResult)
	}
	if r.Answer.IsZero() {
		return ToolResult{CallID: callID, ResultJSON: mustJSON(map[string]any{"fragments": 0})}, nil, nil
	}
	body := map[string]any{
		"answer": r.Answer.ID(), "fragments": r.Fragments, "claims": r.Claims, "supported": r.Supported,
		"unsupported": r.Unsupported, "missing": r.Missing, "sources": r.Sources,
	}
	res := ToolResult{CallID: callID, ResultJSON: mustJSON(body)}
	if err := res.Validate(); err != nil {
		return ToolResult{}, nil, err
	}
	return res, []Ref{r.Answer}, nil
}
