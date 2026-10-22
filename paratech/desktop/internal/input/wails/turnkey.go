package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

// turnkeyError is an error of the turnkey cards: a fixed Russian text the page shows as it is.
type turnkeyError string

func (e turnkeyError) Error() string { return string(e) }

// Errors of the turnkey cards. Causes are logged, never shown.
const (
	ErrTurnkeyNotFound    = turnkeyError("Работа не найдена: возможно, её отменили.")
	ErrTurnkeyInvalid     = turnkeyError("Это действие сейчас недоступно: обновите карточку.")
	ErrTurnkeyNoStepHelp  = turnkeyError("Вопросы по шагу пока недоступны.")
	ErrTurnkeyUnavailable = turnkeyError("Не удалось выполнить. Попробуйте ещё раз.")
)

// Problems of a work view after a reader call that did not go through; the work is saved, its call waits.
const (
	turnkeyProblemOffline = "offline" // «Нет связи с paratech — повторите позже»
	turnkeyProblemQuota   = "quota"
	turnkeyProblemRefused = "refused" // the answer was refused; «Повторить» asks again (a new paid attempt)
	turnkeyProblemNoText  = "no_text"
	turnkeyProblemGone    = "source_gone"
)

// turnkeyMessages are the texts of the problems.
var turnkeyMessages = map[string]string{
	turnkeyProblemOffline: "Нет связи с paratech: разбор не завершён. Повторите позже.",
	turnkeyProblemQuota:   "Лимит запросов исчерпан: разбор не завершён.",
	turnkeyProblemRefused: "Ответ разбора не прошёл проверку. Можно повторить.",
	turnkeyProblemNoText:  "В файле задания нет текста, который можно прочитать.",
	turnkeyProblemGone:    "Задание или файл удалены.",
}

// Operation names of the log.
const (
	opTurnkeyWork    = "ui: turnkey work"
	opTurnkeyAnswer  = "ui: turnkey answers"
	opTurnkeyEdit    = "ui: turnkey card edit"
	opTurnkeyConfirm = "ui: turnkey confirm requirements"
	opTurnkeyStep    = "ui: turnkey step question"
)

// TurnkeyUseCase is the turnkey work (cases/turnkey) as its cards and the helper page use it (ARCH-42).
type TurnkeyUseCase interface {
	Work(ctx context.Context, id shared.WorkID) (turnkey.View, error)
	WorkOf(ctx context.Context, src work.Source) (turnkey.View, error)
	AnswerQuestions(ctx context.Context, id shared.WorkID, replies []work.Reply) (turnkey.View, error)
	SkipQuestions(ctx context.Context, id shared.WorkID) (turnkey.View, error)
	EditUnderstanding(ctx context.Context, id shared.WorkID, u work.Understanding) (turnkey.View, error)
	ConfirmRequirements(ctx context.Context, id shared.WorkID) (turnkey.View, error)
	Retry(ctx context.Context, id shared.WorkID) (turnkey.View, error)
	AskStep(ctx context.Context, id shared.WorkID, step int, question string) (string, error)
}

// TurnkeyWork is a turnkey work for the QuestionsCard, the card «Как я понял задание», the CannotDoCard and the helper
// page (TASK-130). Every text but Message is the reader's or the student's: render it as plain text only.
//
// Status is triage (a reader call waits: Problem tells why, «Повторить» is Retry), questions, review, confirmed,
// writing, done, helper or failed. Questions are set in questions (QuestionRound 1 or 2); Card and Edited once the
// reader answered (HasCard); Helper in the helper mode and for a partial work (its Placeholders).
type TurnkeyWork struct {
	ID                string            `json:"id"`
	AssignmentID      string            `json:"assignmentId"`
	MaterialID        string            `json:"materialId"`
	Status            string            `json:"status"`
	Round             int               `json:"round"`
	QuestionRound     int               `json:"questionRound"`
	Questions         []TurnkeyQuestion `json:"questions"`
	HasCard           bool              `json:"hasCard"`
	Card              TurnkeyCard       `json:"card"`
	Edited            []string          `json:"edited"`
	Feasibility       string            `json:"feasibility"` // full, partial, impossible; "" before an answer
	FeasibilityReason string            `json:"feasibilityReason"`
	Partial           bool              `json:"partial"`
	Assumptions       []string          `json:"assumptions"`
	Helper            *TurnkeyHelper    `json:"helper"`
	Answered          []TurnkeyAnswered `json:"answered"`
	Version           int64             `json:"version"`
	Problem           string            `json:"problem"` // "", offline, quota, refused, no_text, source_gone
	Message           string            `json:"message"`
}

// TurnkeyQuestion is one question: kind choice (options, default index) or text (defaultText; "" may be skipped).
type TurnkeyQuestion struct {
	Kind        string   `json:"kind"`
	Text        string   `json:"text"`
	Options     []string `json:"options"`
	Default     int      `json:"default"`
	DefaultText string   `json:"defaultText"`
	Why         string   `json:"why"`
}

// TurnkeyCard is «Как я понял задание»; workType is referat, doklad, essay, answers, problems, lab_report,
// presentation or other; the requirements are one line each, "" unknown.
type TurnkeyCard struct {
	WorkType   string `json:"workType"`
	Volume     string `json:"volume"`
	FileFormat string `json:"fileFormat"`
	Formatting string `json:"formatting"`
	Variant    string `json:"variant"`
	Deadline   string `json:"deadline"`
	Sources    string `json:"sources"`
}

// TurnkeyHelper is the helper mode: steps, what to submit and how, typical pitfalls, the parts the app can make, the
// placeholders the student fills.
type TurnkeyHelper struct {
	Steps        []string `json:"steps"`
	Submit       string   `json:"submit"`
	Pitfalls     []string `json:"pitfalls"`
	ReadyParts   []string `json:"readyParts"`
	Placeholders []string `json:"placeholders"`
}

// TurnkeyAnswered is a question of an earlier round with the student's answer ("" skipped; Default: its default).
type TurnkeyAnswered struct {
	Round    int    `json:"round"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Default  bool   `json:"default"`
}

// TurnkeyAnswer is the student's answer to question number Question (from 0): Option for a choice, Text for a text
// question. A question left out takes its default.
type TurnkeyAnswer struct {
	Question int    `json:"question"`
	Option   int    `json:"option"`
	Text     string `json:"text"`
}

// TurnkeyService is bound to the turnkey cards and the helper page: every exported method is callable from the page
// by its full name. None of them sends, exports or writes the work: AnswerQuestions, SkipQuestions and Retry ask the
// reader (a paid call), the rest change only local data.
type TurnkeyService struct {
	core TurnkeyUseCase
	log  *slog.Logger
}

// NewTurnkeyService returns the binding; OpenServices checks the use case before calling it.
func NewTurnkeyService(core TurnkeyUseCase, log *slog.Logger) *TurnkeyService {
	return &TurnkeyService{core: core, log: log}
}

// Work returns a work by its ID (the workId of a work card).
func (s *TurnkeyService) Work(ctx context.Context, id string) (TurnkeyWork, error) {
	v, err := s.core.Work(ctx, shared.WorkID(id))
	return s.reply(opTurnkeyWork, v, err)
}

// WorkOfAssignment returns the work of an assignment, for the card on the assignment's page; none is
// ErrTurnkeyNotFound.
func (s *TurnkeyService) WorkOfAssignment(ctx context.Context, assignmentID string) (TurnkeyWork, error) {
	v, err := s.core.WorkOf(ctx, work.Source{Assignment: shared.AssignmentID(assignmentID)})
	return s.reply(opTurnkeyWork, v, err)
}

// AnswerQuestions sends the answers of the QuestionsCard; the next round is asked at once.
func (s *TurnkeyService) AnswerQuestions(ctx context.Context, id string, answers []TurnkeyAnswer) (TurnkeyWork, error) {
	replies := make([]work.Reply, 0, len(answers))
	for _, a := range answers {
		replies = append(replies, work.Reply{Question: a.Question, Option: a.Option, Text: a.Text})
	}
	v, err := s.core.AnswerQuestions(ctx, shared.WorkID(id), replies)
	return s.reply(opTurnkeyAnswer, v, err)
}

// SkipQuestions is «Пропустить — сделай как считаешь»: every question takes its default.
func (s *TurnkeyService) SkipQuestions(ctx context.Context, id string) (TurnkeyWork, error) {
	v, err := s.core.SkipQuestions(ctx, shared.WorkID(id))
	return s.reply(opTurnkeyAnswer, v, err)
}

// EditUnderstanding saves the student's card «Как я понял задание» (kept locally).
func (s *TurnkeyService) EditUnderstanding(ctx context.Context, id string, card TurnkeyCard) (TurnkeyWork, error) {
	v, err := s.core.EditUnderstanding(ctx, shared.WorkID(id), work.Understanding{
		WorkType: work.WorkType(card.WorkType),
		Requirements: work.Requirements{
			Volume: card.Volume, FileFormat: card.FileFormat, Formatting: card.Formatting, Variant: card.Variant,
			Deadline: card.Deadline, Sources: card.Sources,
		},
	})
	return s.reply(opTurnkeyEdit, v, err)
}

// ConfirmRequirements is «Подтвердить» on the card «Как я понял задание».
func (s *TurnkeyService) ConfirmRequirements(ctx context.Context, id string) (TurnkeyWork, error) {
	v, err := s.core.ConfirmRequirements(ctx, shared.WorkID(id))
	return s.reply(opTurnkeyConfirm, v, err)
}

// Retry asks the waiting reader call again («Повторить»).
func (s *TurnkeyService) Retry(ctx context.Context, id string) (TurnkeyWork, error) {
	v, err := s.core.Retry(ctx, shared.WorkID(id))
	return s.reply(opTurnkeyAnswer, v, err)
}

// TurnkeyStepAnswer is the answer about a helper step: plain text only.
type TurnkeyStepAnswer struct {
	Text string `json:"text"`
}

// AskStep is «Спросить по шагу»: a question about helper step number step (from 0).
func (s *TurnkeyService) AskStep(ctx context.Context, id string, step int, question string) (TurnkeyStepAnswer, error) {
	text, err := s.core.AskStep(ctx, shared.WorkID(id), step, question)
	switch {
	case err == nil:
		return TurnkeyStepAnswer{Text: text}, nil
	case errors.Is(err, turnkey.ErrNotConfigured):
		return TurnkeyStepAnswer{}, ErrTurnkeyNoStepHelp
	}
	return TurnkeyStepAnswer{}, s.fail(opTurnkeyStep, err)
}

// reply maps the outcome of a use case call: a work whose reader call did not go through comes back with its
// problem; other errors are a fixed text.
func (s *TurnkeyService) reply(op string, v turnkey.View, err error) (TurnkeyWork, error) {
	if err == nil {
		return workView(v), nil
	}
	if problem := turnkeyProblemOf(err); problem != "" && v.ID != "" {
		s.log.Info(op, "work", string(v.ID), "problem", problem, "err", err)
		out := workView(v)
		out.Problem, out.Message = problem, turnkeyMessages[problem]
		return out, nil
	}
	return TurnkeyWork{}, s.fail(op, err)
}

// turnkeyProblemOf is the problem of a reader call that kept the work; "" for another error.
func turnkeyProblemOf(err error) string {
	switch {
	case errors.Is(err, turnkey.ErrReaderUnavailable):
		return turnkeyProblemOffline
	case errors.Is(err, turnkey.ErrQuota):
		return turnkeyProblemQuota
	case errors.Is(err, turnkey.ErrReaderRefused):
		return turnkeyProblemRefused
	case errors.Is(err, turnkey.ErrNoText):
		return turnkeyProblemNoText
	case errors.Is(err, turnkey.ErrSourceNotFound):
		return turnkeyProblemGone
	}
	return ""
}

func (s *TurnkeyService) fail(op string, err error) error {
	s.log.Warn(op, "err", err)
	switch {
	case errors.Is(err, turnkey.ErrNotFound):
		return ErrTurnkeyNotFound
	case errors.Is(err, turnkey.ErrInvalidRequest):
		return ErrTurnkeyInvalid
	}
	return ErrTurnkeyUnavailable
}

// workView maps a work; empty lists are [] for the page.
func workView(v turnkey.View) TurnkeyWork {
	out := TurnkeyWork{
		ID: string(v.ID), AssignmentID: string(v.Source.Assignment), MaterialID: string(v.Source.Material),
		Status: string(v.Status), Round: v.Round, QuestionRound: v.QuestionRound, Questions: []TurnkeyQuestion{},
		HasCard: v.HasAnswer, Edited: []string{}, Feasibility: string(v.Feasibility),
		FeasibilityReason: v.FeasibilityReason, Partial: v.Partial, Assumptions: nonNilStrings(v.Assumptions),
		Answered: []TurnkeyAnswered{}, Version: v.Version,
	}
	for _, q := range v.Questions {
		out.Questions = append(out.Questions, TurnkeyQuestion{
			Kind: string(q.Kind), Text: q.Text, Options: nonNilStrings(q.Options), Default: q.DefaultOption,
			DefaultText: q.DefaultText, Why: q.Why,
		})
	}
	if v.HasAnswer {
		u := v.Understanding
		out.Card = TurnkeyCard{
			WorkType: string(u.WorkType), Volume: u.Requirements.Volume, FileFormat: u.Requirements.FileFormat,
			Formatting: u.Requirements.Formatting, Variant: u.Requirements.Variant, Deadline: u.Requirements.Deadline,
			Sources: u.Requirements.Sources,
		}
	}
	for _, f := range v.Edited {
		out.Edited = append(out.Edited, string(f))
	}
	if h := v.Helper; h != nil {
		out.Helper = &TurnkeyHelper{
			Steps: nonNilStrings(h.Steps), Submit: h.Submit, Pitfalls: nonNilStrings(h.Pitfalls),
			ReadyParts: nonNilStrings(h.ReadyParts), Placeholders: nonNilStrings(h.Placeholders),
		}
	}
	for _, a := range v.Answered {
		out.Answered = append(out.Answered, TurnkeyAnswered{Round: a.Round, Question: a.Question, Answer: a.Answer, Default: a.Default})
	}
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return append([]string(nil), s...)
}
