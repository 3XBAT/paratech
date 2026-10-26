package values

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// CardKind is the type of a card in the agent panel (ADR-0021, design §2.5).
type CardKind string

// Card kinds.
const (
	CardPlan       CardKind = "plan"
	CardQuestions  CardKind = "questions"
	CardProgress   CardKind = "progress"
	CardResult     CardKind = "result"
	CardConfirm    CardKind = "confirm"
	CardActionLine CardKind = "action_line"
	CardAnswer     CardKind = "answer"
	CardCannotDo   CardKind = "cannot_do"
	CardReply      CardKind = "reply"
	CardStopped    CardKind = "stopped"
	CardWork       CardKind = "work"      // a turnkey work: questions, «Как я понял задание» or the helper mode (TASK-129)
	CardFreeWork   CardKind = "free_work" // a work on a topic waits for the student's «Сделать» (TASK-188)
)

// Card is a typed card. Texts other than the model fields of show_plan, reply, cannot_do and ask_questions are
// desktop templates (impersonal voice); the other cards carry only IDs and enums.
type Card interface {
	Kind() CardKind
	Validate() error
}

// MaxQuestionRounds is the number of question rounds a request may have; a third is refused (ADR-0021).
const MaxQuestionRounds = 2

// PlanCard is «Понял так»: a caption and the structured steps, approved with «Да» by the student.
type PlanCard struct {
	Summary Text
	Steps   []PlanStep
	Notes   []Text
}

// Kind returns CardPlan.
func (PlanCard) Kind() CardKind { return CardPlan }

// Validate checks the card.
func (c PlanCard) Validate() error {
	if _, err := ParseText(string(c.Summary)); err != nil {
		return fmt.Errorf("%w: plan summary: %w", ErrInvalidCard, err)
	}
	if len(c.Steps) == 0 || len(c.Steps) > MaxPlanSteps {
		return fmt.Errorf("%w: a plan has 1..%d steps", ErrInvalidCard, MaxPlanSteps)
	}
	for _, s := range c.Steps {
		if !s.tool.Valid() {
			return fmt.Errorf("%w: plan step with an unknown tool", ErrInvalidCard)
		}
	}
	return validTexts(c.Notes, "plan notes")
}

// QuestionsCard asks the student before planning further; Round is 1 or 2.
type QuestionsCard struct {
	Round     int
	Questions []Question
}

// Kind returns CardQuestions.
func (QuestionsCard) Kind() CardKind { return CardQuestions }

// Validate checks the card.
func (c QuestionsCard) Validate() error {
	if c.Round < 1 || c.Round > MaxQuestionRounds {
		return fmt.Errorf("%w: question round must be 1..%d", ErrInvalidCard, MaxQuestionRounds)
	}
	if len(c.Questions) == 0 || len(c.Questions) > MaxQuestions {
		return fmt.Errorf("%w: a round has 1..%d questions", ErrInvalidCard, MaxQuestions)
	}
	for _, q := range c.Questions {
		if err := q.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ProgressCard shows a running job (snapshot from activity, checkpoint N/M).
type ProgressCard struct {
	Job shared.JobID
}

// Kind returns CardProgress.
func (ProgressCard) Kind() CardKind { return CardProgress }

// Validate checks the card.
func (c ProgressCard) Validate() error {
	if err := c.Job.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCard, err)
	}
	return nil
}

// ResultCard opens a finished artifact (summary, .docx, assignment).
type ResultCard struct {
	Artifact shared.ArtifactRef
}

// Kind returns CardResult.
func (ResultCard) Kind() CardKind { return CardResult }

// Validate checks the card.
func (c ResultCard) Validate() error {
	if c.Artifact.IsZero() {
		return fmt.Errorf("%w: no artifact", ErrInvalidCard)
	}
	return nil
}

// ConfirmCard is bound to a proposal or a deletion preview only: the recipient, attachment and subject shown come
// from it (trusted), never from model text. The action runs only on the student's click in input/wails.
type ConfirmCard struct {
	Target Ref
}

// Kind returns CardConfirm.
func (ConfirmCard) Kind() CardKind { return CardConfirm }

// Validate checks the card.
func (c ConfirmCard) Validate() error {
	if c.Target.Kind() != KindProposal && c.Target.Kind() != KindPreview {
		return fmt.Errorf("%w: a confirm card refers to a proposal or a preview", ErrInvalidCard)
	}
	return nil
}

// ActionLine is one journal line with «Отменить». Assignment is the assignment create_assignment added (ADR-0029),
// zero for other tools: the action names its ID, and the Driver fills its title, subject and deadline from the local
// data before the line is shown (the planner never sees them).
type ActionLine struct {
	Action     shared.ActionID
	Assignment AssignmentLine
}

// Kind returns CardActionLine.
func (ActionLine) Kind() CardKind { return CardActionLine }

// Validate checks the card: an assignment, when there is one, is a valid line with a title.
func (c ActionLine) Validate() error {
	if err := c.Action.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCard, err)
	}
	if c.Assignment == (AssignmentLine{}) {
		return nil
	}
	if c.Assignment.Assignment.Kind() != KindAssignment {
		return fmt.Errorf("%w: an action line names an assignment", ErrInvalidCard)
	}
	return c.Assignment.validate()
}

// AnswerCard shows an answer with verified citations by its ID.
type AnswerCard struct {
	Answer Ref
}

// Kind returns CardAnswer.
func (AnswerCard) Kind() CardKind { return CardAnswer }

// Validate checks the card.
func (c AnswerCard) Validate() error {
	if c.Answer.Kind() != KindAnswer {
		return fmt.Errorf("%w: an answer card refers to an answer", ErrInvalidCard)
	}
	return nil
}

// CannotDoCard is «Целиком не получится»: what the student does and what the app can do.
type CannotDoCard struct {
	YourPart []Text
	OurPart  []Text
}

// Kind returns CardCannotDo.
func (CannotDoCard) Kind() CardKind { return CardCannotDo }

// Validate checks the card.
func (c CannotDoCard) Validate() error {
	if len(c.YourPart) == 0 && len(c.OurPart) == 0 {
		return fmt.Errorf("%w: a cannot-do card is empty", ErrInvalidCard)
	}
	if err := validTexts(c.YourPart, "your part"); err != nil {
		return err
	}
	return validTexts(c.OurPart, "our part")
}

// ReplyCard is a short answer of the planner (reply). Assignments are the IDs the reply lists (catalog v3), each
// passed the provenance check; Lines are what the desktop shows of them, filled from local data by the Driver
// (ADR-0022): the planner never sees or writes a title.
type ReplyCard struct {
	Text        Text
	Assignments []Ref
	Lines       []AssignmentLine
}

// AssignmentLine is one assignment a reply lists (or an action line shows), as the local data has it: its title
// (trusted local data, shown as plain text), its library subject (zero when none) and its deadline (zero when none).
// DueTime is the time of day of the deadline, set on an action line only (zero on a reply's lines).
type AssignmentLine struct {
	Assignment Ref
	Title      string
	Subject    Ref
	Due        Date
	DueTime    TimeOfDay
}

func (l AssignmentLine) validate() error {
	if strings.TrimSpace(l.Title) == "" || !utf8.ValidString(l.Title) {
		return fmt.Errorf("%w: a line without a title", ErrInvalidCard)
	}
	if !l.Subject.IsZero() && l.Subject.Kind() != KindSubject {
		return fmt.Errorf("%w: a line subject is not a subject", ErrInvalidCard)
	}
	if !l.DueTime.IsZero() && l.Due.IsZero() {
		return fmt.Errorf("%w: a line time without a date", ErrInvalidCard)
	}
	return nil
}

// Kind returns CardReply.
func (ReplyCard) Kind() CardKind { return CardReply }

// Validate checks the card: at most MaxCardIDs distinct assignment IDs, and lines only of those, each with a title.
func (c ReplyCard) Validate() error {
	if _, err := ParseText(string(c.Text)); err != nil {
		return fmt.Errorf("%w: reply: %w", ErrInvalidCard, err)
	}
	if len(c.Assignments) > MaxCardIDs {
		return fmt.Errorf("%w: a reply lists at most %d assignments", ErrInvalidCard, MaxCardIDs)
	}
	listed := RefSet{}
	for _, r := range c.Assignments {
		if r.Kind() != KindAssignment || listed.Has(r) {
			return fmt.Errorf("%w: a reply lists distinct assignment ids", ErrInvalidCard)
		}
		listed.Add(r)
	}
	if len(c.Lines) > len(c.Assignments) {
		return fmt.Errorf("%w: more lines than listed assignments", ErrInvalidCard)
	}
	shown := RefSet{}
	for _, l := range c.Lines {
		if !listed.Has(l.Assignment) || shown.Has(l.Assignment) {
			return fmt.Errorf("%w: a line of an assignment the reply does not list", ErrInvalidCard)
		}
		shown.Add(l.Assignment)
		if err := l.validate(); err != nil {
			return err
		}
	}
	return nil
}

// StopReason is why a request stopped before it was done.
type StopReason string

// Stop reasons.
const (
	StopBudget  StopReason = "budget"  // «Остановлено: лимит просьбы»; cannot be continued
	StopOffline StopReason = "offline" // «Не завершено: нет связи · Продолжить»
	StopUser    StopReason = "user"    // the student pressed «Стоп»; may be continued
	StopQuota   StopReason = "quota"   // the quota is used up; «Продолжить» repeats the same key later
	StopFailed  StopReason = "failed"  // the step failed for good; «Повторить» sends it again with a new key
	StopExpired StopReason = "expired" // the Hub no longer keeps the model turn; the request starts again from its text
)

// Valid reports whether r is one of the reasons.
func (r StopReason) Valid() bool {
	switch r {
	case StopBudget, StopOffline, StopUser, StopQuota, StopFailed, StopExpired:
		return true
	}
	return false
}

// StoppedCard tells why a request stopped.
type StoppedCard struct {
	Reason StopReason
}

// Kind returns CardStopped.
func (StoppedCard) Kind() CardKind { return CardStopped }

// Validate checks the card.
func (c StoppedCard) Validate() error {
	if !c.Reason.Valid() {
		return fmt.Errorf("%w: unknown stop reason", ErrInvalidCard)
	}
	return nil
}

func validTexts(texts []Text, what string) error {
	if len(texts) > MaxTextListItems {
		return fmt.Errorf("%w: %s has more than %d items", ErrInvalidCard, what, MaxTextListItems)
	}
	for _, t := range texts {
		if _, err := ParseText(string(t)); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrInvalidCard, what, err)
		}
	}
	return nil
}
