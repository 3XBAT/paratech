package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/recordingdelete"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

// agentConfirmError is an error of the agent's confirm card: a fixed Russian text the panel shows as it is.
type agentConfirmError string

func (e agentConfirmError) Error() string { return string(e) }

// Errors of the confirm card. Causes are logged, never shown. A send reuses the texts of the homework page.
const (
	ErrAgentConfirmInvalid     = agentConfirmError("Неверная карточка подтверждения: обновите панель.")
	ErrAgentConfirmNotFound    = agentConfirmError("Предложение не найдено: возможно, оно уже обработано.")
	ErrAgentConfirmDecided     = agentConfirmError("Это предложение уже подтверждено или отклонено.")
	ErrAgentConfirmDeleteHere  = agentConfirmError("Удаление записи подтверждается в окне удаления.")
	ErrAgentConfirmCalendar    = agentConfirmError("Календарь не подключён: подключите Google в настройках и нажмите «Да» ещё раз.")
	ErrAgentConfirmNoDue       = agentConfirmError("У задания нет срока: укажите его в «Домашних заданиях».")
	ErrAgentConfirmUnavailable = agentConfirmError("Не удалось выполнить. Нажмите «Да» ещё раз — повторного действия не будет.")
	// ErrAgentConfirmNoAssignment is the refusal no_assignment: a work without an assignment is never sent (TASK-180).
	ErrAgentConfirmNoAssignment = agentConfirmError("Работа сделана не по заданию из списка ДЗ: её можно сохранить, но не отправить преподавателю.")
)

// workRejectMessages explain why the send of a work cannot be confirmed; the other reasons read as for a homework send.
var workRejectMessages = map[proposal.RejectReason]string{
	proposal.ReasonNoArtifact: "Нет готового файла работы: дождитесь, пока она допишется, или проверьте, что файл не удалён.",
	proposal.ReasonFileMoved:  "Файл работы убран из папки «ДЗ»: верните его туда и нажмите «Отправить преподавателю» ещё раз.",
}

// Operation names of the log.
const (
	opConfirmCard  = "ui: agent confirm card"
	opProposeDue   = "ui: propose a deadline"
	opProposeWork  = "ui: propose sending a work"
	opAgentConfirm = "ui: agent confirm"
	opAgentDecline = "ui: agent decline"
	opAgentNotice  = "ui: agent confirm card notice"
)

// msgDueOutOfRange explains a deadline that cannot go to the calendar.
const msgDueOutOfRange = "Срок уже прошёл или слишком далеко: в календарь не добавить."

// msgOtherAddress is shown on a send when the reader saw an address in the file the assignment came from (ADR-0022).
const msgOtherAddress = "В файле указан другой адрес — не используется"

// Kinds of a confirm card as the panel reads them.
const (
	agentCardDeadline = "deadline"
	agentCardSend     = "send"
	agentCardDelete   = "delete_recording"
)

// States of a confirm card as the panel reads them.
const (
	agentStateWaiting   = "waiting"   // «Да» / «Нет» are offered
	agentStateConfirmed = "confirmed" // confirmed, the action did not finish: «Да» again repeats it safely
	agentStateDone      = "done"
	agentStateDeclined  = "declined"
	agentStateStale     = "stale"    // what was shown changed: nothing ran
	agentStateRejected  = "rejected" // could not be proposed
)

// AgentConfirmProposals is the proposal pipeline (cases/homeworkflow) as the confirm card uses it (ARCH-42). Only the
// student's click calls ConfirmSendTo, ConfirmSendWorkTo, ConfirmDeadline and Decline; input/agent has no such
// method, so the agent never chooses a teacher (hard rule 3).
type AgentConfirmProposals interface {
	ProposeDeadline(ctx context.Context, a shared.AssignmentID) (homeworkflow.ProposalView, error)
	ProposeSendWork(ctx context.Context, w shared.WorkID) (homeworkflow.ProposalView, error)
	ConfirmSendWorkTo(ctx context.Context, id proposal.ID, shownSubject string, shownFormat proposal.Format,
		chosen homeworkflow.ChosenTeacher) (homeworkflow.ProposalView, error)
	Get(ctx context.Context, id proposal.ID) (homeworkflow.ProposalView, error)
	Assignment(ctx context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error)
	ConfirmSendTo(ctx context.Context, id proposal.ID, shownSubject string, shownFormat proposal.Format,
		chosen homeworkflow.ChosenTeacher) (homeworkflow.ProposalView, error)
	ConfirmDeadline(ctx context.Context, id proposal.ID) (homeworkflow.ProposalView, error)
	Decline(ctx context.Context, id proposal.ID) (homeworkflow.ProposalView, error)
}

// AgentConfirmMaterials reads the reader's flag of a library material (cases/library).
type AgentConfirmMaterials interface {
	MentionsEmail(ctx context.Context, id shared.MaterialID) (bool, error)
}

// AgentConfirmDeletions previews a recording deletion (cases/recordingdelete); the deletion itself is the existing
// RecordingDeleteService.Delete of the deletion window.
type AgentConfirmDeletions interface {
	Preview(ctx context.Context, id shared.RecordingID) (recordingdelete.Preview, error)
}

// AgentConfirmCore is what the confirm card binds to; every field is required.
type AgentConfirmCore struct {
	Proposals AgentConfirmProposals
	Materials AgentConfirmMaterials
	Deletions AgentConfirmDeletions
}

func (c AgentConfirmCore) complete() bool {
	return c.Proposals != nil && c.Materials != nil && c.Deletions != nil
}

// AgentConfirmCard is the data of a confirm card (TASK-127, the card is TASK-128), read from the proposal or the
// deletion preview only, never from the agent's text: Kind "deadline", "send" or "delete_recording"; State "waiting",
// "confirmed", "done", "declined", "stale" or "rejected" with Message for the last two.
//
// Deadline: Title (the assignment as stored, plain text), Subject, Due. Send: Recipient (a teacher of the subject,
// with RecipientName, Role, Reason, SubjectID and Options as described in homeworkrecipient.go), EmailSubject,
// AttachmentName, AttachmentSize, AttachmentFormat and DraftVersion (0: not a draft version as such), Notice «В файле
// указан другой адрес — не используется» when the reader saw an address in the source file. Confirm sends
// EmailSubject, AttachmentFormat and the chosen option back. Delete: Deletion, confirmed through RecordingDeleteService.
type AgentConfirmCard struct {
	Kind             string           `json:"kind"`
	TargetKind       string           `json:"targetKind"`
	TargetID         string           `json:"targetId"`
	State            string           `json:"state"`
	Message          string           `json:"message"`
	AssignmentID     string           `json:"assignmentId"`
	Title            string           `json:"title"`
	Subject          string           `json:"subject"`
	Due              string           `json:"due"`
	Recipient        string           `json:"recipient"`
	RecipientName    string           `json:"recipientName"`
	Role             string           `json:"role"`
	Reason           string           `json:"reason"`
	SubjectID        string           `json:"subjectId"`
	Options          []TeacherOption  `json:"options"`
	EmailSubject     string           `json:"emailSubject"`
	AttachmentName   string           `json:"attachmentName"`
	AttachmentSize   int64            `json:"attachmentSize"`
	AttachmentFormat string           `json:"attachmentFormat"`
	DraftVersion     int              `json:"draftVersion"`
	Notice           string           `json:"notice"`
	Deletion         *DeleteRecording `json:"deletion"`
}

// AgentConfirmService is bound to the confirm cards of the agent panel: every exported method is callable from the
// page by its full name. Confirm is the only method that leads to an email or a calendar event, and only on the
// student's click; the agent loop (input/agent) cannot reach it.
type AgentConfirmService struct {
	core AgentConfirmCore
	log  *slog.Logger
}

// NewAgentConfirmService returns the binding of the confirm cards; OpenServices checks the core before calling it.
func NewAgentConfirmService(c AgentConfirmCore, log *slog.Logger) *AgentConfirmService {
	return &AgentConfirmService{core: c, log: log}
}

// Card returns the data of the card bound to target (kind "proposal" or "preview", the target of a confirm card).
func (s *AgentConfirmService) Card(ctx context.Context, kind, id string) (AgentConfirmCard, error) {
	switch values.IDKind(kind) {
	case values.KindProposal:
		pid, err := proposalID(id)
		if err != nil {
			return AgentConfirmCard{}, err
		}
		v, err := s.core.Proposals.Get(ctx, pid)
		if err != nil {
			return AgentConfirmCard{}, s.fail(opConfirmCard, err)
		}
		return s.proposalCard(ctx, v)
	case values.KindPreview:
		rid := shared.RecordingID(id)
		if rid.Validate() != nil {
			return AgentConfirmCard{}, ErrAgentConfirmInvalid
		}
		p, err := s.core.Deletions.Preview(ctx, rid)
		if err != nil {
			return AgentConfirmCard{}, s.fail(opConfirmCard, err)
		}
		view := deleteRecordingView(p)
		card := AgentConfirmCard{
			Kind: agentCardDelete, TargetKind: kind, TargetID: id, State: agentStateWaiting, Deletion: &view,
		}
		if !view.Deletable {
			card.State, card.Message = agentStateRejected, view.RefusalMessage
		}
		return card, nil
	}
	return AgentConfirmCard{}, ErrAgentConfirmInvalid
}

// ProposeDeadline is «Да, в календарь» of a post on «Сегодня» (TASK-125): the deadline of the stored assignment
// becomes a proposal, or the one that already waits is returned; the card then offers «Да» (Confirm). Nothing goes to
// the calendar here.
func (s *AgentConfirmService) ProposeDeadline(ctx context.Context, assignmentID string) (AgentConfirmCard, error) {
	aid := shared.AssignmentID(assignmentID)
	if aid.Validate() != nil {
		return AgentConfirmCard{}, ErrAgentConfirmInvalid
	}
	v, err := s.core.Proposals.ProposeDeadline(ctx, aid)
	if errors.Is(err, homeworkflow.ErrNoDue) {
		s.log.Info(opProposeDue, "result", err)
		return AgentConfirmCard{}, ErrAgentConfirmNoDue
	}
	if err != nil {
		return AgentConfirmCard{}, s.fail(opProposeDue, err)
	}
	return s.proposalCard(ctx, v)
}

// ProposeSendWork is «Отправить преподавателю» on a finished work (TASK-180), the student's action only; the agent
// never calls it. The send of the work's file becomes a proposal, or the one that already waits is returned; the card
// (kind "send") then offers «Да» (Confirm). A work without an assignment is refused with ErrAgentConfirmNoAssignment
// (no_assignment). Nothing is sent here.
func (s *AgentConfirmService) ProposeSendWork(ctx context.Context, workID string) (AgentConfirmCard, error) {
	wid := shared.WorkID(workID)
	if wid.Validate() != nil {
		return AgentConfirmCard{}, ErrAgentConfirmInvalid
	}
	v, err := s.core.Proposals.ProposeSendWork(ctx, wid)
	if errors.Is(err, work.ErrNoAssignment) {
		s.log.Info(opProposeWork, "result", "no_assignment")
		return AgentConfirmCard{}, ErrAgentConfirmNoAssignment
	}
	if err != nil {
		return AgentConfirmCard{}, s.fail(opProposeWork, err)
	}
	return s.proposalCard(ctx, v)
}

// Confirm is «Да» on a proposal card: a send is confirmed with the subject and the attachment format the card
// showed (another one by now makes it stale, nothing is sent), a deadline is put into the calendar. A second click is
// safe: no second email and no second event (hard rule 7); an email whose outcome is unknown is never sent again here,
// it is resolved in the list of emails. A deletion preview is confirmed in the deletion window instead.
//
// role and address are the teacher the student clicked when a send card asks «Кому отправить?» (Reason
// "choose_teacher") and the address that option showed; "" and "" when the card asked nothing. Without a choice such
// a send is refused with ErrSendChooseTeacher; another current address of the role makes it stale.
func (s *AgentConfirmService) Confirm(ctx context.Context, kind, id, emailSubject, format, role, address string) (AgentConfirmCard, error) {
	switch values.IDKind(kind) {
	case values.KindPreview:
		return AgentConfirmCard{}, ErrAgentConfirmDeleteHere
	case values.KindProposal:
	default:
		return AgentConfirmCard{}, ErrAgentConfirmInvalid
	}
	pid, err := proposalID(id)
	if err != nil {
		return AgentConfirmCard{}, err
	}
	v, err := s.core.Proposals.Get(ctx, pid)
	if err != nil {
		return AgentConfirmCard{}, s.fail(opAgentConfirm, err)
	}
	switch v.Kind {
	case proposal.KindSendHomework:
		f, ferr := sendFormat(format)
		if ferr != nil {
			return AgentConfirmCard{}, ferr
		}
		v, err = s.core.Proposals.ConfirmSendTo(ctx, pid, emailSubject, f, chosenTeacher(role, address))
	case proposal.KindSendWork:
		f, ok := workFormats[format]
		if !ok {
			return AgentConfirmCard{}, ErrAgentConfirmInvalid
		}
		v, err = s.core.Proposals.ConfirmSendWorkTo(ctx, pid, emailSubject, f, chosenTeacher(role, address))
	case proposal.KindDeadlineEvent:
		v, err = s.core.Proposals.ConfirmDeadline(ctx, pid)
	default:
		s.log.Warn(opAgentConfirm, "result", "not an agent proposal", "kind", v.Kind)
		return AgentConfirmCard{}, ErrAgentConfirmInvalid
	}
	if err != nil {
		return AgentConfirmCard{}, s.confirmError(err)
	}
	return s.proposalCard(ctx, v)
}

// Decline is «Нет» on a proposal card: it can never run afterwards. A preview has nothing stored to decline.
func (s *AgentConfirmService) Decline(ctx context.Context, kind, id string) (AgentConfirmCard, error) {
	switch values.IDKind(kind) {
	case values.KindPreview:
		if shared.RecordingID(id).Validate() != nil {
			return AgentConfirmCard{}, ErrAgentConfirmInvalid
		}
		return AgentConfirmCard{Kind: agentCardDelete, TargetKind: kind, TargetID: id, State: agentStateDeclined}, nil
	case values.KindProposal:
	default:
		return AgentConfirmCard{}, ErrAgentConfirmInvalid
	}
	pid, err := proposalID(id)
	if err != nil {
		return AgentConfirmCard{}, err
	}
	v, err := s.core.Proposals.Decline(ctx, pid)
	switch {
	case errors.Is(err, proposal.ErrInvalidTransition):
		s.log.Info(opAgentDecline, "result", err)
		return AgentConfirmCard{}, ErrAgentConfirmDecided
	case err != nil:
		return AgentConfirmCard{}, s.fail(opAgentDecline, err)
	}
	return s.proposalCard(ctx, v)
}

// proposalCard is the card of a proposal view; the assignment's labels and the notice are read again.
func (s *AgentConfirmService) proposalCard(ctx context.Context, v homeworkflow.ProposalView) (AgentConfirmCard, error) {
	card := AgentConfirmCard{
		TargetKind: string(values.KindProposal), TargetID: string(v.ID), State: proposalState(v.Status),
		AssignmentID: string(v.AssignmentID),
	}
	switch v.Kind {
	case proposal.KindDeadlineEvent:
		card.Kind, card.Title, card.Due = agentCardDeadline, v.Title, timeText(v.Due)
	case proposal.KindSendHomework, proposal.KindSendWork:
		card.Kind = agentCardSend
		card.Recipient, card.EmailSubject, card.DraftVersion = v.Recipient, v.Subject, v.DraftVersion
		card.RecipientName, card.Role, card.Reason = v.RecipientName, string(v.RecipientRole), string(v.RecipientReason)
		card.SubjectID, card.Options = string(v.SubjectID), teacherOptions(v)
		card.AttachmentName, card.AttachmentSize, card.AttachmentFormat = v.AttachmentName, v.AttachmentSize, string(v.AttachmentFormat)
	default:
		s.log.Warn(opConfirmCard, "result", "not an agent proposal", "kind", v.Kind)
		return AgentConfirmCard{}, ErrAgentConfirmInvalid
	}
	switch v.Status {
	case proposal.StatusRejected:
		card.Message = msgRejectedDefault
		if msg, ok := rejectMessages[v.Reason]; ok {
			card.Message = msg
		}
		if msg, ok := workRejectMessages[v.Reason]; ok && v.Kind == proposal.KindSendWork {
			card.Message = msg
		}
		if v.Reason == proposal.ReasonDueOutOfRange {
			card.Message = msgDueOutOfRange
		}
		if msg, ok := recipientMessage(v.Reason, v.RecipientRole, ""); ok {
			card.Message = msg // named with the subject below when the assignment is read
		}
	case proposal.StatusStale:
		card.Message = string(ErrSendStale)
	}

	a, err := s.core.Proposals.Assignment(ctx, v.AssignmentID)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return card, nil
	case err != nil:
		return AgentConfirmCard{}, s.fail(opConfirmCard, err)
	}
	card.Subject = a.Subject
	if msg, ok := recipientMessage(v.Reason, v.RecipientRole, a.Subject); ok && v.Status == proposal.StatusRejected {
		card.Message = msg
	}
	if card.Kind == agentCardDeadline && card.Title == "" {
		card.Title = a.Title
	}
	if card.Kind == agentCardSend {
		if card.Title == "" {
			card.Title = a.Title
		}
		card.Notice = s.notice(ctx, a.MaterialID)
	}
	return card, nil
}

// notice is «В файле указан другой адрес — не используется» when the reader saw an address in the material the
// assignment came from. The address goes nowhere: the recipient is always a teacher of the subject in the library. A
// failed read only loses the hint.
func (s *AgentConfirmService) notice(ctx context.Context, m shared.MaterialID) string {
	if m == "" {
		return ""
	}
	mentions, err := s.core.Materials.MentionsEmail(ctx, m)
	if err != nil {
		s.log.Warn(opAgentNotice, "err", err)
		return ""
	}
	if mentions {
		return msgOtherAddress
	}
	return ""
}

func proposalState(st proposal.Status) string {
	switch st {
	case proposal.StatusValidated:
		return agentStateWaiting
	case proposal.StatusConfirmed:
		return agentStateConfirmed
	case proposal.StatusExecuted:
		return agentStateDone
	case proposal.StatusDeclined:
		return agentStateDeclined
	case proposal.StatusStale:
		return agentStateStale
	}
	return agentStateRejected
}

// confirmError maps an error of a confirmation to the text the card shows.
func (s *AgentConfirmService) confirmError(err error) error {
	switch {
	case errors.Is(err, shared.ErrNotFound), errors.Is(err, homeworkflow.ErrNotASend), errors.Is(err, homeworkflow.ErrNotADeadline):
		s.log.Info(opAgentConfirm, "result", err)
		return ErrAgentConfirmNotFound
	case errors.Is(err, proposal.ErrInvalidTransition):
		s.log.Info(opAgentConfirm, "result", err)
		return ErrAgentConfirmDecided
	case errors.Is(err, proposal.ErrStale), errors.Is(err, proposal.ErrInvalidRecipient):
		s.log.Info(opAgentConfirm, "result", err)
		return ErrSendStale
	case errors.Is(err, proposal.ErrChooseTeacher):
		s.log.Info(opAgentConfirm, "result", "choose_teacher")
		return ErrSendChooseTeacher
	case errors.Is(err, senddelivery.ErrUnresolvedDelivery):
		s.log.Info(opAgentConfirm, "result", err)
		return ErrSendUnresolved
	case errors.Is(err, homeworkflow.ErrCalendarNotConnected):
		s.log.Info(opAgentConfirm, "result", err)
		return ErrAgentConfirmCalendar
	}
	s.log.Error(opAgentConfirm, "err", err)
	return ErrAgentConfirmUnavailable
}

// fail maps a missing item to ErrAgentConfirmNotFound and anything else to ErrAgentConfirmUnavailable.
func (s *AgentConfirmService) fail(op string, err error) error {
	if errors.Is(err, shared.ErrNotFound) || errors.Is(err, recordingdelete.ErrNotFound) {
		s.log.Info(op, "result", "not found")
		return ErrAgentConfirmNotFound
	}
	s.log.Error(op, "err", err)
	return ErrAgentConfirmUnavailable
}

// workFormats are the attachment formats of the send of a work as the card names them.
var workFormats = map[string]proposal.Format{"docx": proposal.FormatDocx, "pptx": proposal.FormatPptx}

func proposalID(id string) (proposal.ID, error) {
	pid := proposal.ID(id)
	if pid.Validate() != nil {
		return "", ErrAgentConfirmInvalid
	}
	return pid, nil
}
