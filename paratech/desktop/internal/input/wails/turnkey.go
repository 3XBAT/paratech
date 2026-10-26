package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/job"
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
	ErrTurnkeyWriting     = turnkeyError("Работа ещё пишется: дождитесь окончания.")
	ErrTurnkeyWritten     = turnkeyError("Работа уже написана: измените план или сгенерируйте раздел заново.")
	ErrTurnkeyStale       = turnkeyError("Работа изменилась: обновите карточку.")
	ErrTurnkeyNotStopped  = turnkeyError("Не удалось остановить написание. Попробуйте ещё раз.")
	ErrTurnkeyNoSource    = turnkeyError("Задание не найдено: возможно, его удалили.")

	// AskStep: a question whose call did not go through; the work is not touched.
	ErrTurnkeyStepOffline = turnkeyError("Нет связи с paratech: вопрос не отправлен. Повторите позже.")
	ErrTurnkeyStepQuota   = turnkeyError("Лимит запросов исчерпан: ответить на вопрос сейчас нельзя.")
	ErrTurnkeyStepRefused = turnkeyError("Ответ на вопрос не прошёл проверку. Спросите ещё раз или переформулируйте вопрос.")
)

// States of the writing job (TurnkeyWriting.JobState) while it is active.
const (
	turnkeyJobQueued         = "queued"
	turnkeyJobRunning        = "running"
	turnkeyJobWaitingNetwork = "waiting_network"
	turnkeyJobWaitingQuota   = "waiting_quota"
	turnkeyJobWaitingPower   = "waiting_power"
	turnkeyJobRetrying       = "retrying"
)

// turnkeyJobStates maps the active job statuses; a terminal one is no active job.
var turnkeyJobStates = map[job.Status]string{
	job.StatusQueued:            turnkeyJobQueued,
	job.StatusRunning:           turnkeyJobRunning,
	job.StatusWaitingForNetwork: turnkeyJobWaitingNetwork,
	job.StatusWaitingForQuota:   turnkeyJobWaitingQuota,
	job.StatusWaitingForPower:   turnkeyJobWaitingPower,
	job.StatusRetryWait:         turnkeyJobRetrying,
}

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
	opTurnkeyStart   = "ui: turnkey start"
	opTurnkeyWork    = "ui: turnkey work"
	opTurnkeyAnswer  = "ui: turnkey answers"
	opTurnkeyEdit    = "ui: turnkey card edit"
	opTurnkeyConfirm = "ui: turnkey confirm requirements"
	opTurnkeyStep    = "ui: turnkey step question"
	opTurnkeyWrite   = "ui: turnkey writing"
	opTurnkeyOutline = "ui: turnkey outline"
	opTurnkeyStop    = "ui: turnkey stop writing"
)

// TurnkeyUseCase is the turnkey work (cases/turnkey) as its cards and the helper page use it (ARCH-42).
type TurnkeyUseCase interface {
	Triage(ctx context.Context, src work.Source) (turnkey.Started, error)
	Work(ctx context.Context, id shared.WorkID) (turnkey.View, error)
	WorkOf(ctx context.Context, src work.Source) (turnkey.View, error)
	AnswerQuestions(ctx context.Context, id shared.WorkID, replies []work.Reply) (turnkey.View, error)
	SkipQuestions(ctx context.Context, id shared.WorkID) (turnkey.View, error)
	EditUnderstanding(ctx context.Context, id shared.WorkID, u work.Understanding) (turnkey.View, error)
	ConfirmRequirements(ctx context.Context, id shared.WorkID) (turnkey.View, error)
	Retry(ctx context.Context, id shared.WorkID) (turnkey.View, error)
	AskStep(ctx context.Context, id shared.WorkID, step int, question string) (string, error)

	// The writing (TASK-131): the student's own moves, never the agent's.
	StartWriting(ctx context.Context, id shared.WorkID) (turnkey.WritingStarted, error)
	ApproveOutline(ctx context.Context, id shared.WorkID) (turnkey.WritingStarted, error)
	EditOutline(ctx context.Context, id shared.WorkID, items []work.OutlineItem) (turnkey.View, error)
	EditOutlineAt(ctx context.Context, id shared.WorkID, version int64, items []work.OutlineItem) (turnkey.View, error)
	Regenerate(ctx context.Context, id shared.WorkID, number int, note string) (turnkey.WritingStarted, error)
	StopWriting(ctx context.Context, id shared.WorkID) (turnkey.View, error)
}

// TurnkeyWork is a turnkey work for the QuestionsCard, the card «Как я понял задание», the CannotDoCard and the helper
// page (TASK-130). Every text but Message is the reader's or the student's: render it as plain text only.
//
// Status is triage (a reader call waits: Problem tells why, «Повторить» is Retry), questions, review, confirmed,
// writing, done, helper or failed. Questions are set in questions (QuestionRound 1 or 2); Card and Edited once the
// reader answered (HasCard); Helper in the helper mode and for a partial work (its Placeholders); Writing from the
// start of the writing on (TASK-131).
//
// One of AssignmentID, MaterialID and Topic is set (TASK-187): Topic is the text of a work on a topic (the student's or
// the agent's words: plain text only) and SubjectID its library subject ("" none, or a subject removed since: the
// paper goes to «Входящие»). Sendable is true for a work of an assignment only: «Отправить преподавателю» is offered
// for it alone (work.Sendable).
type TurnkeyWork struct {
	ID                string            `json:"id"`
	AssignmentID      string            `json:"assignmentId"`
	MaterialID        string            `json:"materialId"`
	Topic             string            `json:"topic"`
	SubjectID         string            `json:"subjectId"`
	Sendable          bool              `json:"sendable"`
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
	Writing           *TurnkeyWriting   `json:"writing"` // from the start of the writing on
	Version           int64             `json:"version"`
	Problem           string            `json:"problem"` // "", offline, quota, refused, no_text, source_gone
	Message           string            `json:"message"`
}

// TurnkeyWriting is the writing of a work: the outline the student edits and approves, the progress «Раздел N из M»
// and the result. Step is outline (the plan is being written), approve (the plan waits for «Утвердить план»), section,
// assemble, blocked (a step used its attempts: «Сгенерировать заново») or none (done). Active is read with every work:
// a writing job runs for it («Остановить»), JobState queued, running, waiting_network, waiting_quota, waiting_power or
// retrying ("" when not active). StopWriting of a running step leaves it active (running) until the step ends: the
// page keeps polling Work until Active is false. A step other than approve, blocked or none without Active was
// stopped or ended early: «Продолжить» is StartWriting. Every text was written by the model: render it as plain
// text only.
type TurnkeyWriting struct {
	Step       string           `json:"step"`
	Generation int              `json:"generation"`
	Topic      string           `json:"topic"`
	Sections   []TurnkeySection `json:"sections"`
	Done       int              `json:"done"`
	Total      int              `json:"total"`
	SubjectID  string           `json:"subjectId"`  // the subject whose «ДЗ» gets the file; "" «Входящие»
	MaterialID string           `json:"materialId"` // the file in the library, once done
	Result     string           `json:"result"`     // the artifact of the ResultCard, once done
	Job        string           `json:"job"`        // the job the call started, else the active one; "" when none
	Active     bool             `json:"active"`
	JobState   string           `json:"jobState"`
}

// TurnkeySection is a section of the outline; Paragraphs and Sources once written. A source with verify set was named
// by the model, not taken from the student's materials («проверить источник»).
type TurnkeySection struct {
	Number     int             `json:"number"`
	Title      string          `json:"title"`
	Words      int             `json:"words"`
	Note       string          `json:"note"`
	Written    bool            `json:"written"`
	Paragraphs []string        `json:"paragraphs"`
	Sources    []TurnkeySource `json:"sources"`
}

// TurnkeySource is an entry of the bibliography of a section.
type TurnkeySource struct {
	Text       string `json:"text"`
	MaterialID string `json:"materialId"`
	Verify     bool   `json:"verify"`
}

// TurnkeyOutlineItem is a section of the student's outline; number 0 is a new section.
type TurnkeyOutlineItem struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Words  int    `json:"words"`
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
// by its full name. None of them sends or exports the work: StartForAssignment, AnswerQuestions, SkipQuestions and
// Retry ask the reader (a paid call); StartWriting, ApproveOutline and Regenerate start a paid writing job whose paper
// is filed in the local library; the rest change only local data. The student's click is the consent to a paid call:
// unlike the agent's triage_assignment, StartForAssignment leaves no entry in the agent's journal.
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

// StartForAssignment is «Сделать под ключ» on the assignment's page: the same triage as the agent's
// triage_assignment (a paid reader call). An assignment that has a work already (not failed) gets it back without a
// new paid call; a work whose call did not go through comes back with its problem, as with Retry. An assignment
// that does not exist is ErrTurnkeyNoSource.
func (s *TurnkeyService) StartForAssignment(ctx context.Context, assignmentID string) (TurnkeyWork, error) {
	st, err := s.core.Triage(ctx, work.Source{Assignment: shared.AssignmentID(assignmentID)})
	if err != nil && st.View.ID == "" && errors.Is(err, turnkey.ErrSourceNotFound) {
		s.log.Info(opTurnkeyStart, "err", err)
		return TurnkeyWork{}, ErrTurnkeyNoSource
	}
	return s.reply(opTurnkeyStart, st.View, err)
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

// StartWriting is «Написать работу» on a confirmed work (a paid job): the plan is written first and waits for
// ApproveOutline.
func (s *TurnkeyService) StartWriting(ctx context.Context, id string) (TurnkeyWork, error) {
	st, err := s.core.StartWriting(ctx, shared.WorkID(id))
	return s.started(opTurnkeyWrite, st, err)
}

// ApproveOutline is «Утвердить план»: the sections are written (a paid job, «Раздел N из M»).
func (s *TurnkeyService) ApproveOutline(ctx context.Context, id string) (TurnkeyWork, error) {
	st, err := s.core.ApproveOutline(ctx, shared.WorkID(id))
	return s.started(opTurnkeyOutline, st, err)
}

// EditOutline saves the student's plan: the sections in their order, unchanged ones keep their text. A plan that
// waits for the approval still waits; a done work is written again by StartWriting.
func (s *TurnkeyService) EditOutline(ctx context.Context, id string, items []TurnkeyOutlineItem) (TurnkeyWork, error) {
	v, err := s.core.EditOutline(ctx, shared.WorkID(id), outlineOf(items))
	return s.reply(opTurnkeyOutline, v, err)
}

// EditOutlineAt is EditOutline of the plan as the editor showed it at version (the work's version): a work that
// changed since is ErrTurnkeyStale and nothing is saved.
func (s *TurnkeyService) EditOutlineAt(ctx context.Context, id string, version int64, items []TurnkeyOutlineItem) (TurnkeyWork, error) {
	v, err := s.core.EditOutlineAt(ctx, shared.WorkID(id), version, outlineOf(items))
	return s.reply(opTurnkeyOutline, v, err)
}

// StopWriting is «Остановить»: the writing job is cancelled, the sections written stay. A running step stays active
// until it ends (keep polling Work). Without a job nothing changes.
func (s *TurnkeyService) StopWriting(ctx context.Context, id string) (TurnkeyWork, error) {
	v, err := s.core.StopWriting(ctx, shared.WorkID(id))
	return s.reply(opTurnkeyStop, v, err)
}

func outlineOf(items []TurnkeyOutlineItem) []work.OutlineItem {
	outline := make([]work.OutlineItem, 0, len(items))
	for _, it := range items {
		outline = append(outline, work.OutlineItem{Number: it.Number, Title: it.Title, Words: it.Words})
	}
	return outline
}

// Regenerate is «Дополнительные указания → Сгенерировать заново» for section number (0: every section) with the
// student's note (a paid job for those sections only); 0 on a blocked outline asks for the outline again.
func (s *TurnkeyService) Regenerate(ctx context.Context, id string, number int, note string) (TurnkeyWork, error) {
	st, err := s.core.Regenerate(ctx, shared.WorkID(id), number, note)
	return s.started(opTurnkeyWrite, st, err)
}

// started maps the outcome of a call that starts a writing job: the work with the job of its progress card. A
// finished job the call found (not created, not active) is no job of the card.
func (s *TurnkeyService) started(op string, st turnkey.WritingStarted, err error) (TurnkeyWork, error) {
	if err != nil {
		return TurnkeyWork{}, s.fail(op, err)
	}
	out := workView(st.View)
	if out.Writing != nil && (st.Created || out.Writing.Active) {
		out.Writing.Job = string(st.Job)
	}
	return out, nil
}

// TurnkeyStepAnswer is the answer about a helper step: plain text only.
type TurnkeyStepAnswer struct {
	Text string `json:"text"`
}

// AskStep is «Спросить по шагу»: a question about helper step number step (from 0). No connection, the quota and a
// refused answer have texts of their own (ErrTurnkeyStepOffline, ErrTurnkeyStepQuota, ErrTurnkeyStepRefused): asking
// again is a new try.
func (s *TurnkeyService) AskStep(ctx context.Context, id string, step int, question string) (TurnkeyStepAnswer, error) {
	text, err := s.core.AskStep(ctx, shared.WorkID(id), step, question)
	switch {
	case err == nil:
		return TurnkeyStepAnswer{Text: text}, nil
	case errors.Is(err, turnkey.ErrNotConfigured):
		return TurnkeyStepAnswer{}, ErrTurnkeyNoStepHelp
	case errors.Is(err, turnkey.ErrReaderUnavailable):
		s.log.Info(opTurnkeyStep, "err", err)
		return TurnkeyStepAnswer{}, ErrTurnkeyStepOffline
	case errors.Is(err, turnkey.ErrQuota):
		s.log.Info(opTurnkeyStep, "err", err)
		return TurnkeyStepAnswer{}, ErrTurnkeyStepQuota
	case errors.Is(err, turnkey.ErrReaderRefused):
		s.log.Info(opTurnkeyStep, "err", err)
		return TurnkeyStepAnswer{}, ErrTurnkeyStepRefused
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
	case errors.Is(err, turnkey.ErrWritingRunning):
		return ErrTurnkeyWriting
	case errors.Is(err, turnkey.ErrAlreadyWritten):
		return ErrTurnkeyWritten
	case errors.Is(err, turnkey.ErrChanged):
		return ErrTurnkeyStale
	case errors.Is(err, turnkey.ErrNotStopped):
		return ErrTurnkeyNotStopped
	case errors.Is(err, turnkey.ErrInvalidRequest), errors.Is(err, turnkey.ErrNotWritable),
		errors.Is(err, turnkey.ErrWritingRetries):
		return ErrTurnkeyInvalid
	}
	return ErrTurnkeyUnavailable
}

// workView maps a work; empty lists are [] for the page.
func workView(v turnkey.View) TurnkeyWork {
	out := TurnkeyWork{
		ID: string(v.ID), AssignmentID: string(v.Source.Assignment), MaterialID: string(v.Source.Material),
		Topic: v.Source.Topic.Text(), SubjectID: string(v.Source.Topic.Subject()), Sendable: v.Sendable,
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
	out.Writing = writingView(v.Writing)
	return out
}

// writingView maps the writing of a work; nil before it starts, empty lists are [].
func writingView(w *turnkey.WritingView) *TurnkeyWriting {
	if w == nil {
		return nil
	}
	out := &TurnkeyWriting{
		Step: string(w.Step), Generation: w.Generation, Topic: w.Topic, Sections: []TurnkeySection{}, Done: w.Done,
		Total: w.Total, SubjectID: string(w.Subject), MaterialID: string(w.Material), Result: w.Result.String(),
	}
	if state, ok := turnkeyJobStates[w.JobState]; ok && w.Active {
		out.Job, out.Active, out.JobState = string(w.Job), true, state
	}
	for _, s := range w.Sections {
		sec := TurnkeySection{
			Number: s.Number, Title: s.Title, Words: s.Words, Note: s.Note, Written: s.Written,
			Paragraphs: nonNilStrings(s.Paragraphs), Sources: []TurnkeySource{},
		}
		for _, r := range s.Sources {
			sec.Sources = append(sec.Sources, TurnkeySource{Text: r.Text, MaterialID: string(r.Material), Verify: r.Material == ""})
		}
		out.Sections = append(out.Sections, sec)
	}
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return append([]string(nil), s...)
}
