package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/tickets"
	"paratech/desktop/internal/domain/shared"
)

// ticketsError is an error of the tickets screen: a fixed Russian text the page shows as it is.
type ticketsError string

func (e ticketsError) Error() string { return string(e) }

// Errors of the tickets screen. Causes are logged, never shown.
const (
	ErrTicketsNotFound      = ticketsError("Билеты не найдены: возможно, их отменили.")
	ErrTicketsNoTickets     = ticketsError("Билеты не найдены: нужен нумерованный список («1.», «1)» или «Билет 1»).")
	ErrTicketsTooMany       = ticketsError("Слишком много билетов: не больше 100 за один раз.")
	ErrTicketsListTooLong   = ticketsError("Список слишком длинный.")
	ErrTicketsTicketTooLong = ticketsError("Один из билетов длиннее 500 символов: сократите его.")
	ErrTicketsNoSubject     = ticketsError("Сначала положите файл со списком в предмет.")
	ErrTicketsSubject       = ticketsError("Предмет не найден.")
	ErrTicketsSource        = ticketsError("Файл со списком удалён.")
	ErrTicketsNoText        = ticketsError("В файле нет текста, который можно прочитать.")
	ErrTicketsCost          = ticketsError("Прогон дороже обычного: подтвердите стоимость.")
	ErrTicketsDone          = ticketsError("Ответы уже готовы: файл в папке «Билеты».")
	ErrTicketsChanged       = ticketsError("Файл уже перемещён или удалён: оставлен как есть.")
	ErrTicketsInvalid       = ticketsError("Это действие сейчас недоступно: обновите экран.")
	ErrTicketsUnavailable   = ticketsError("Не удалось выполнить. Попробуйте ещё раз.")
)

// Operation names of the log.
const (
	opTicketsPropose = "ui: tickets propose"
	opTicketsGet     = "ui: tickets get"
	opTicketsConfirm = "ui: tickets confirm"
	opTicketsCancel  = "ui: tickets cancel"
	opTicketsUndo    = "ui: tickets undo filing"
)

// TicketsUseCase is the tickets use case (cases/tickets) as the tickets screen uses it (ARCH-42). Every move here is
// the student's: the agent has no port to them.
type TicketsUseCase interface {
	Propose(ctx context.Context, sub shared.SubjectID, text string) (tickets.View, error)
	ProposeFromMaterial(ctx context.Context, id shared.MaterialID) (tickets.View, error)
	Set(ctx context.Context, id shared.TicketSetID) (tickets.View, error)
	Confirm(ctx context.Context, id shared.TicketSetID, acceptCost bool) (tickets.Started, error)
	Cancel(ctx context.Context, id shared.TicketSetID) (tickets.View, error)
	Unfile(ctx context.Context, id shared.TicketSetID) (tickets.View, error)
}

// TicketSet is a set of exam tickets for the screen (TASK-134): «Найдено N билетов — подтвердить», the progress
// «Билет N из M», the coverage report and the file. Titles come from the student's list: render them as plain text
// only. Status is proposed, confirmed, answering, done, failed or cancelled.
type TicketSet struct {
	ID           string       `json:"id"`
	SubjectID    string       `json:"subjectId"`
	SourceID     string       `json:"sourceId"` // the material of the list; "" pasted
	Status       string       `json:"status"`
	Tickets      []TicketItem `json:"tickets"`
	Done         int          `json:"done"`
	Total        int          `json:"total"`
	Budget       TicketBudget `json:"budget"`
	CostAccepted bool         `json:"costAccepted"`
	Report       TicketReport `json:"report"`
	MaterialID   string       `json:"materialId"` // the file in «Билеты» (open it through the library), once done
	Result       string       `json:"result"`     // the artifact of the ResultCard, once done
	Job          string       `json:"job"`        // the job Confirm started (the progress card); "" otherwise
	Version      int64        `json:"version"`
}

// TicketItem is one ticket: coverage covered, partial, not_covered or failed once answered; answerId opens the
// answer card (AnswerService.Answer).
type TicketItem struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Answered bool   `json:"answered"`
	Coverage string `json:"coverage"`
	AnswerID string `json:"answerId"`
}

// TicketBudget is the estimate of the run the confirm card shows; overCap: Confirm needs acceptCost.
type TicketBudget struct {
	Tickets          int   `json:"tickets"`
	EstimateMicroUSD int64 `json:"estimateMicroUsd"`
	CapMicroUSD      int64 `json:"capMicroUsd"`
	OverCap          bool  `json:"overCap"`
}

// TicketReport counts the tickets by coverage: «N из ваших лекций / частично / нет в ваших лекциях».
type TicketReport struct {
	Covered    int `json:"covered"`
	Partial    int `json:"partial"`
	NotCovered int `json:"notCovered"`
	Failed     int `json:"failed"`
	Pending    int `json:"pending"`
}

// TicketService is bound to the tickets screen: every exported method is callable from the page by its full name.
// Propose and ProposeFromMaterial pay nothing; only Confirm starts the paid run, whose file is filed in the local
// library; none of them sends or exports anything.
type TicketService struct {
	core TicketsUseCase
	log  *slog.Logger
}

// NewTicketService returns the binding; OpenServices checks the use case before calling it.
func NewTicketService(core TicketsUseCase, log *slog.Logger) *TicketService {
	return &TicketService{core: core, log: log}
}

// Propose reads a pasted list for a subject: «Найдено N билетов — подтвердить».
func (s *TicketService) Propose(ctx context.Context, subjectID, text string) (TicketSet, error) {
	v, err := s.core.Propose(ctx, shared.SubjectID(subjectID), text)
	return s.reply(opTicketsPropose, v, err)
}

// ProposeFromMaterial reads the list of a library material (filed under its subject).
func (s *TicketService) ProposeFromMaterial(ctx context.Context, materialID string) (TicketSet, error) {
	v, err := s.core.ProposeFromMaterial(ctx, shared.MaterialID(materialID))
	return s.reply(opTicketsPropose, v, err)
}

// Get returns a set by its ID.
func (s *TicketService) Get(ctx context.Context, id string) (TicketSet, error) {
	v, err := s.core.Set(ctx, shared.TicketSetID(id))
	return s.reply(opTicketsGet, v, err)
}

// Confirm is «Подтвердить» (a paid run, «Билет N из M»); acceptCost is the student's «Да, подтверждаю стоимость» of a
// run over the cap.
func (s *TicketService) Confirm(ctx context.Context, id string, acceptCost bool) (TicketSet, error) {
	st, err := s.core.Confirm(ctx, shared.TicketSetID(id), acceptCost)
	if err != nil {
		return TicketSet{}, s.fail(opTicketsConfirm, err)
	}
	out := ticketSetView(st.View)
	out.Job = string(st.Job)
	return out, nil
}

// Cancel is «Отменить»: the run stops; the answers already made stay.
func (s *TicketService) Cancel(ctx context.Context, id string) (TicketSet, error) {
	v, err := s.core.Cancel(ctx, shared.TicketSetID(id))
	return s.reply(opTicketsCancel, v, err)
}

// Undo moves the filed file back from «Билеты» to «Входящие» (the file kept), unless the student moved it.
func (s *TicketService) Undo(ctx context.Context, id string) (TicketSet, error) {
	v, err := s.core.Unfile(ctx, shared.TicketSetID(id))
	return s.reply(opTicketsUndo, v, err)
}

func (s *TicketService) reply(op string, v tickets.View, err error) (TicketSet, error) {
	if err != nil {
		return TicketSet{}, s.fail(op, err)
	}
	return ticketSetView(v), nil
}

func (s *TicketService) fail(op string, err error) error {
	s.log.Warn(op, "err", err)
	for _, m := range []struct {
		cause error
		text  ticketsError
	}{
		{tickets.ErrNotFound, ErrTicketsNotFound},
		{tickets.ErrNoTickets, ErrTicketsNoTickets},
		{tickets.ErrTooManyTickets, ErrTicketsTooMany},
		{tickets.ErrListTooLong, ErrTicketsListTooLong},
		{tickets.ErrTicketTooLong, ErrTicketsTicketTooLong},
		{tickets.ErrNoSubject, ErrTicketsNoSubject},
		{tickets.ErrSubjectNotFound, ErrTicketsSubject},
		{tickets.ErrSourceNotFound, ErrTicketsSource},
		{tickets.ErrNoText, ErrTicketsNoText},
		{tickets.ErrCostNotAccepted, ErrTicketsCost},
		{tickets.ErrAlreadyDone, ErrTicketsDone},
		{tickets.ErrChanged, ErrTicketsChanged},
		{tickets.ErrInvalidRequest, ErrTicketsInvalid},
		{tickets.ErrRetries, ErrTicketsInvalid},
	} {
		if errors.Is(err, m.cause) {
			return m.text
		}
	}
	return ErrTicketsUnavailable
}

// ticketSetView maps a set; empty lists are [] for the page.
func ticketSetView(v tickets.View) TicketSet {
	out := TicketSet{
		ID: string(v.ID), SubjectID: string(v.Subject), SourceID: string(v.Source), Status: string(v.Status),
		Tickets: []TicketItem{}, Done: v.Done, Total: v.Total, CostAccepted: v.CostAccepted,
		Budget: TicketBudget{
			Tickets: v.Budget.Tickets, EstimateMicroUSD: v.Budget.EstimateMicroUSD, CapMicroUSD: v.Budget.CapMicroUSD,
			OverCap: v.Budget.OverCap,
		},
		Report: TicketReport{
			Covered: v.Report.Covered, Partial: v.Report.Partial, NotCovered: v.Report.NotCovered, Failed: v.Report.Failed,
			Pending: v.Report.Pending,
		},
		MaterialID: string(v.Material), Result: v.Result.String(), Version: v.Version,
	}
	for _, t := range v.Tickets {
		out.Tickets = append(out.Tickets, TicketItem{
			Number: t.Number, Title: t.Title, Answered: t.Answered, Coverage: string(t.Coverage), AnswerID: string(t.Answer),
		})
	}
	return out
}
