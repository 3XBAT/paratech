package values

import (
	"fmt"
	"slices"
)

// TriageOutcome is where a turnkey work stands after triage_assignment, as the planner and the panel see it.
type TriageOutcome string

// Triage outcomes.
const (
	TriageQuestions TriageOutcome = "questions" // the student answers the reader's questions (QuestionsCard)
	TriageReview    TriageOutcome = "review"    // «Как я понял задание» waits for the student's confirmation
	TriageHelper    TriageOutcome = "helper"    // cannot be done whole: the helper mode (CannotDoCard)
	TriagePending   TriageOutcome = "pending"   // the reader call did not finish (no network); «Повторить»
	TriageFailed    TriageOutcome = "failed"    // the reader kept refusing
	TriageConfirmed TriageOutcome = "confirmed" // the requirements were confirmed earlier (or the work went further)
)

// TriageOutcomes returns every outcome.
func TriageOutcomes() []TriageOutcome {
	return []TriageOutcome{TriageQuestions, TriageReview, TriageHelper, TriagePending, TriageFailed, TriageConfirmed}
}

// feasibilities are the reader's feasibility enum (TRIAGE_ASSIGNMENT).
var feasibilities = []string{"full", "partial", "impossible"}

// TriageRow is the result of triage_assignment (ADR-0022): the work, where it stands, the reader's feasibility enum
// ("" before an answer), how many questions wait and the round. No text of the assignment, the questions or the
// reader's answer is in it; those go to the cards only.
type TriageRow struct {
	Work        Ref
	Outcome     TriageOutcome
	Feasibility string
	Questions   int
	Round       int
	Changed     bool // false: the work existed already and nothing was done for it
}

// TriageResult is the result of triage_assignment and the IDs it shows.
func TriageResult(callID string, r TriageRow) (ToolResult, []Ref, error) {
	if err := wantRef(r.Work, KindWork, true); err != nil {
		return ToolResult{}, nil, err
	}
	switch {
	case !slices.Contains(TriageOutcomes(), r.Outcome):
		return ToolResult{}, nil, fmt.Errorf("%w: unknown triage outcome", ErrInvalidResult)
	case r.Feasibility != "" && !slices.Contains(feasibilities, r.Feasibility):
		return ToolResult{}, nil, fmt.Errorf("%w: unknown feasibility", ErrInvalidResult)
	case r.Questions < 0 || r.Questions > MaxTriageQuestions || (r.Questions > 0) != (r.Outcome == TriageQuestions):
		return ToolResult{}, nil, fmt.Errorf("%w: questions wait only in the questions outcome", ErrInvalidResult)
	case r.Round < 1 || r.Round > MaxQuestionRounds+1:
		return ToolResult{}, nil, fmt.Errorf("%w: round out of range", ErrInvalidResult)
	}
	body := map[string]any{
		"work": r.Work.ID(), "outcome": string(r.Outcome), "questions": r.Questions, "round": r.Round, "changed": r.Changed,
	}
	if r.Feasibility != "" {
		body["feasibility"] = r.Feasibility
	}
	res := ToolResult{CallID: callID, ResultJSON: mustJSON(body)}
	if err := res.Validate(); err != nil {
		return ToolResult{}, nil, err
	}
	return res, []Ref{r.Work}, nil
}

// MaxTriageQuestions bounds the questions of one triage round (the reader's limit, TRIAGE_ASSIGNMENT).
const MaxTriageQuestions = 5

// WorkCard points the panel to a turnkey work: its QuestionsCard, its card «Как я понял задание» or its CannotDoCard
// and helper page, by Outcome. The texts are read through the turnkey binding of input/wails, never carried here:
// the card holds only the ID and the enum.
type WorkCard struct {
	Work    Ref
	Outcome TriageOutcome
}

// Kind returns CardWork.
func (WorkCard) Kind() CardKind { return CardWork }

// Validate checks the card.
func (c WorkCard) Validate() error {
	if c.Work.Kind() != KindWork {
		return fmt.Errorf("%w: a work card refers to a work", ErrInvalidCard)
	}
	if !slices.Contains(TriageOutcomes(), c.Outcome) {
		return fmt.Errorf("%w: unknown work outcome", ErrInvalidCard)
	}
	return nil
}
