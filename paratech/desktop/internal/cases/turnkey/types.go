package turnkey

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports of the service; every one except Advisor and Log is required.
type Deps struct {
	Works       WorkRepository
	Assignments Assignments
	Materials   MaterialTexts
	Reader      Reader
	Advisor     StepAdvisor // nil: AskStep answers ErrNotConfigured
	Clock       Clock
	IDs         IDs
	Log         *slog.Logger
}

// View is a work as the cards and the helper page show it (TASK-130). Every text the reader wrote is untrusted and
// shown as plain text only.
type View struct {
	ID      shared.WorkID
	Source  work.Source
	Status  work.Status
	Round   int // the round of the pending or last reader call, 1..3
	Attempt int // the attempt of the pending reader call

	// QuestionRound (1 or 2) and Questions are set while questions wait (QuestionsCard).
	QuestionRound int
	Questions     []work.Question

	// The card «Как я понял задание»: the reader's last answer with the student's edits over it.
	HasAnswer     bool
	Understanding work.Understanding
	Edited        []work.Field

	Feasibility       work.Feasibility
	FeasibilityReason string
	Partial           bool         // only a part can be done: placeholders for the student (TASK-107)
	Assumptions       []string     // stated assumptions of the last answer («Предположил вариант 3 — поправьте»)
	Helper            *work.Helper // the helper mode (CannotDoCard, the helper page); nil for a work done whole

	Answered    []Answered // the questions of the earlier rounds with the student's answers
	Instruction string     // the student's channel of the writing, once confirmed (TASK-094)

	Version   int64
	UpdatedAt time.Time
}

// Answered is a question of an earlier round with the student's answer.
type Answered struct {
	Round    int
	Question string
	Answer   string // the chosen option or the typed text; "" skipped
	Default  bool   // the question's default was taken («Пропустить» or left unanswered)
}

// Started is the outcome of Triage: the work and whether this call created it.
type Started struct {
	View    View
	Created bool
}

// viewOf builds the view of a work.
func viewOf(w *work.Work) View {
	v := View{
		ID: w.ID(), Source: w.Source(), Status: w.Status(), Round: w.Round(), Attempt: w.Attempt(),
		Partial: w.Partial(), Instruction: w.Instruction(), Version: w.Version(), UpdatedAt: w.UpdatedAt(),
	}
	v.QuestionRound, v.Questions = w.Questions()
	if a, ok := w.Latest(); ok {
		v.HasAnswer = true
		v.Understanding, v.Edited = w.Understanding()
		v.Feasibility, v.FeasibilityReason, v.Assumptions = a.Feasibility, a.FeasibilityReason, a.Assumptions
		if a.Helper != nil {
			h := *a.Helper
			v.Helper = &h
		}
	}
	for _, r := range w.Rounds() {
		for i, q := range r.Answer.Questions {
			if r.Replies == nil {
				break
			}
			rep := r.Replies[i]
			v.Answered = append(v.Answered, Answered{
				Round: r.Number, Question: q.Text, Answer: work.AnswerText(q, rep), Default: rep.Default,
			})
		}
	}
	return v
}
