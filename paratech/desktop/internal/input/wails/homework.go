package wails

import (
	"context"
	"errors"
	"log/slog"
	"path"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// homeworkError is an error of the homework page: a fixed Russian text the page shows as it is (like modelError).
type homeworkError string

func (e homeworkError) Error() string { return string(e) }

// Errors of the homework page (frontend/src/bindings/homework.ts lists them). Causes are logged, never shown: they
// may name local paths or database details.
const (
	ErrHomeworkUnavailable   = homeworkError("Не удалось выполнить действие. Попробуйте ещё раз.")
	ErrHomeworkInvalidID     = homeworkError("Неверный идентификатор.")
	ErrHomeworkNotFound      = homeworkError("Задание не найдено: обновите список.")
	ErrDraftVersionNotFound  = homeworkError("Такой версии решения нет: обновите страницу.")
	ErrDraftStale            = homeworkError("Решение изменилось: появилась новая версия. Обновите страницу и повторите.")
	ErrDraftInvalid          = homeworkError("Заголовок: одна строка до 200 символов, текст: до 16000 символов; оба не пустые.")
	ErrDraftFull             = homeworkError("У решения уже 100 версий: больше добавить нельзя.")
	ErrInstructionInvalid    = homeworkError("Инструкция слишком длинная (больше 4 КБ) или содержит недопустимые символы.")
	ErrGenerateRetries       = homeworkError("Эта генерация уже много раз не удалась. Измените инструкцию или текст решения.")
	ErrGenerationRunning     = homeworkError("Решение уже готовится. Дождитесь окончания и повторите.")
	ErrSendFormat            = homeworkError("Неизвестный формат вложения: выберите .docx или .txt.")
	ErrGenerationNotFound    = homeworkError("Генерация не найдена: обновите страницу.")
	ErrSendNotFound          = homeworkError("Отправка не найдена: подготовьте её ещё раз.")
	ErrSendCannotConfirm     = homeworkError("Эту отправку уже нельзя подтвердить. Подготовьте её ещё раз.")
	ErrSendAlreadyDecided    = homeworkError("Эта отправка уже подтверждена или отменена.")
	ErrSendStale             = homeworkError("Файл, адрес получателя или тема письма изменились после подтверждения. Проверьте и подтвердите ещё раз.")
	ErrSendUnresolved        = homeworkError("Предыдущее письмо с этим заданием ещё не отправлено до конца или его результат неизвестен. Сначала разберитесь с ним в списке писем.")
	ErrSendIncomplete        = homeworkError("Не удалось завершить отправку. Повторите — второе письмо не уйдёт — или проверьте список писем ниже.")
	ErrExportLocked          = homeworkError("Предыдущее письмо с этим заданием ещё не отправлено до конца или его результат неизвестен. Пока оно не решено, файл задания менять нельзя.")
	ErrDeliveryNotFound      = homeworkError("Письмо не найдено: обновите список.")
	ErrDeliveryActionInvalid = homeworkError("Это действие для письма уже недоступно: обновите список.")
)

// Send preparation statuses as the page reads them.
const (
	sendReady    = "ready"    // the dialog shows recipient, subject and attachment and offers the confirmation
	sendRejected = "rejected" // the dialog shows Message and offers no confirmation
)

// Generation states as the page reads them.
const (
	genGenerating     = "generating"
	genWaitingNetwork = "waitingNetwork"
	genWaitingQuota   = "waitingQuota"
	genFailed         = "failed"
	genDone           = "done"
)

// Fixed texts of the homework page (Russian UI).
const (
	msgUnknownOutcome = "Письмо отправлено, но подтверждение не получено: проверьте сами или отправьте ещё раз под свою ответственность"

	msgWaitingNetwork   = "Нет связи с сервисом конспектов: проверьте интернет и адрес в разделе «Подключения». Генерация продолжится сама."
	msgWaitingQuota     = "Лимит обработки исчерпан. Генерация продолжится, когда лимит обновится."
	msgGenerationFailed = "Не удалось подготовить решение. Попробуйте ещё раз."
	msgGenerationCancel = "Генерация отменена."

	msgRejectedDefault = "Отправить нельзя: подготовьте отправку ещё раз."
	msgNoDocx          = "Нет файла Word с заданием: сначала нажмите «Сохранить в Word (.docx)»."

	msgDocxSaved     = "Файл Word сохранён."
	msgDocxCancelled = "Сохранение отменено."
	msgDocxFailed    = "Не удалось сохранить файл. Выберите другую папку и попробуйте ещё раз."
)

// Statuses of «Сохранить в Word (.docx)» as the page reads them.
const (
	docxSaved     = "saved"
	docxCancelled = "cancelled"
	docxFailed    = "failed"
)

// Attachment formats of a send as the page names them.
const (
	formatTxt  = "txt"
	formatDocx = "docx"
)

// generationMessages explain a failed generation by the error its job ended with.
var generationMessages = []struct {
	err error
	msg string
}{
	{homeworkflow.ErrDraftChanged, "Решение изменилось во время генерации. Сгенерируйте ещё раз."},
	{homeworkflow.ErrDraftFull, string(ErrDraftFull)},
	{homeworkflow.ErrDraftAssignmentGone, "Задание удалено."},
	{homeworkflow.ErrDraftContextRefused, "Задание слишком длинное или содержит недопустимый текст: решение не подготовить."},
	{homeworkflow.ErrDraftInvalidAnswer, "Сервис конспектов ответил в неверном виде. Попробуйте ещё раз."},
}

// rejectMessages explain why a send cannot be confirmed.
var rejectMessages = map[proposal.RejectReason]string{
	proposal.ReasonNoRecipient:       "Не указан адрес получателя: задайте его в разделе «Подключения».",
	proposal.ReasonNoArtifact:        "Нет сохранённого файла с заданием: сначала сохраните решение в файл.",
	proposal.ReasonUnknownAssignment: "Задание не найдено: обновите список.",
}

// Operation names of the log.
const (
	opListAssignments = "ui: list assignments"
	opOpenAssignment  = "ui: open assignment"
	opCorrectLesson   = "ui: correct the lesson of an assignment"
	opDraftVersion    = "ui: get draft version"
	opGenerate        = "ui: generate draft"
	opGeneration      = "ui: get draft generation"
	opEditDraft       = "ui: edit draft"
	opExportDraft     = "ui: export draft"
	opExportDocx      = "ui: export the word file"
	opSaveDocx        = "ui: save the word file"
	opPickDocx        = "ui: choose where to save the word file"
	opDraftStatus     = "ui: draft status"
	opPrepareSend     = "ui: prepare send"
	opConfirmSend     = "ui: confirm send"
	opCancelSend      = "ui: cancel send"
	opDeliveries      = "ui: list deliveries"
	opMarkSent        = "ui: mark delivery as sent"
	opSendAgain       = "ui: send delivery again"
)

// HomeworkProposals is the part of homeworkflow.Service the page uses (ARCH-42): the assignments and the send
// proposal. ConfirmSend is called only by the binding of the same name, on the user's click in the confirmation dialog.
type HomeworkProposals interface {
	ListAssignments(ctx context.Context) ([]homeworkflow.AssignmentView, error)
	Assignment(ctx context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error)
	CorrectLesson(ctx context.Context, c homeworkflow.LessonCorrection) (homeworkflow.AssignmentView, error)
	ProposeSend(ctx context.Context, a shared.AssignmentID, format proposal.Format) (homeworkflow.ProposalView, error)
	Get(ctx context.Context, id proposal.ID) (homeworkflow.ProposalView, error)
	ConfirmSend(ctx context.Context, id proposal.ID, shownSubject string, shownFormat proposal.Format) (homeworkflow.ProposalView, error)
	Decline(ctx context.Context, id proposal.ID) (homeworkflow.ProposalView, error)
}

// HomeworkDrafts is the part of homeworkflow.Drafts the page uses (ARCH-42).
type HomeworkDrafts interface {
	DraftVersions(ctx context.Context, a shared.AssignmentID) ([]homeworkflow.DraftVersionView, error)
	DraftVersion(ctx context.Context, a shared.AssignmentID, n int) (homeworkflow.DraftVersionView, error)
	GenerateFrom(ctx context.Context, a shared.AssignmentID, base int, instruction string) (homeworkflow.GenerateResult, error)
	Generation(ctx context.Context, id shared.JobID) (homeworkflow.GenerationView, error)
	ActiveGeneration(ctx context.Context, a shared.AssignmentID) (homeworkflow.GenerationView, bool, error)
	EditDraft(ctx context.Context, e homeworkflow.DraftEdit) (homeworkflow.DraftVersionView, error)
	ExportDraft(ctx context.Context, a shared.AssignmentID, n int) (homeworkflow.DraftExport, error)
	ExportDocx(ctx context.Context, a shared.AssignmentID, n int) (homeworkflow.DraftExport, error)
	SaveDocx(ctx context.Context, a shared.AssignmentID, n int, path string) error
	DocxName(ctx context.Context, a shared.AssignmentID) (string, error)
}

// DocxFilePicker shows the native save dialog of a Word file with name proposed. It returns the chosen path, or ""
// when the user cancels; the path never comes from the page.
type DocxFilePicker interface {
	PickDocxFile(ctx context.Context, name string) (string, error)
}

// docxPickerFunc adapts a function to DocxFilePicker.
type docxPickerFunc func(ctx context.Context, name string) (string, error)

// PickDocxFile implements DocxFilePicker.
func (f docxPickerFunc) PickDocxFile(ctx context.Context, name string) (string, error) {
	return f(ctx, name)
}

// HomeworkDeliveries is the part of senddelivery.Service the page uses (ARCH-42): the deliveries of an assignment and
// the user's resolution of an unknown outcome.
type HomeworkDeliveries interface {
	ListForAssignment(ctx context.Context, a shared.AssignmentID) ([]senddelivery.View, error)
	Subject() string
	MarkSent(ctx context.Context, id shared.DeliveryID) (senddelivery.View, error)
	SendAgain(ctx context.Context, id shared.DeliveryID) (senddelivery.View, error)
}

// HomeworkCore is what the homework page binds to; every field is required.
type HomeworkCore struct {
	Proposals  HomeworkProposals
	Drafts     HomeworkDrafts
	Deliveries HomeworkDeliveries
}

func (c HomeworkCore) complete() bool {
	return c.Proposals != nil && c.Drafts != nil && c.Deliveries != nil
}

// AssignmentItem is the JSON form of an assignment. Title and description may be model text: the page shows them
// as text only. Times are RFC 3339 in UTC, "" when unset.
//
// The lesson (TASK-092): Subject and Teacher with their source ("schedule", "transcript", "user" or "" when
// unknown); a "transcript" one is a model hint from the recording, shown marked «по записи». Kind is the code of the
// lesson kind ("lecture", "seminar", "practice", "lab" or ""). LessonDate is the lesson start («занятие 19.10»).
// CorrectedSubject and CorrectedTeacher are the user's correction, "" when none, for the correction form.
type AssignmentItem struct {
	ID               string `json:"id"`
	LessonID         string `json:"lessonId"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	Due              string `json:"due"`
	UserEdited       bool   `json:"userEdited"`
	UpdatedAt        string `json:"updatedAt"`
	Subject          string `json:"subject"`
	SubjectSource    string `json:"subjectSource"`
	Kind             string `json:"kind"`
	Teacher          string `json:"teacher"`
	TeacherSource    string `json:"teacherSource"`
	LessonDate       string `json:"lessonDate"`
	CorrectedSubject string `json:"correctedSubject"`
	CorrectedTeacher string `json:"correctedTeacher"`
}

// VersionItem is one draft version in the history list.
type VersionItem struct {
	Number      int    `json:"number"`
	Source      string `json:"source"` // "generated" or "edited"
	Instruction string `json:"instruction"`
	Latest      bool   `json:"latest"`
	CreatedAt   string `json:"createdAt"`
}

// VersionText is one draft version with its text. Title, body and instruction are model or student text: the page
// shows them as text only, never as HTML.
type VersionText struct {
	Number      int    `json:"number"`
	Source      string `json:"source"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Instruction string `json:"instruction"`
	Latest      bool   `json:"latest"`
	CreatedAt   string `json:"createdAt"`
}

// AssignmentDetail is an opened assignment: its versions in order, the text of the latest one (null: no draft) and
// the generation that is pending or running (null: none; TASK-094), so the page shows «Готовим решение…» and offers
// no second generation also after it was left and opened again.
type AssignmentDetail struct {
	Assignment AssignmentItem    `json:"assignment"`
	Versions   []VersionItem     `json:"versions"`
	Latest     *VersionText      `json:"latest"`
	Generation *ActiveGeneration `json:"generation"`
}

// ActiveGeneration is a generation that is pending or running: its job (for GenerationStatus) and its state as
// GenerationStatus gives it ("generating", "waitingNetwork" or "waitingQuota").
type ActiveGeneration struct {
	JobID   string `json:"jobId"`
	State   string `json:"state"`
	Message string `json:"message"`
	Percent int    `json:"percent"`
}

// DraftStatus is the cheap state of the draft of an assignment for the page to poll (TASK-094): the number of the
// latest version (0: no draft; a larger one than shown means a new version to load) and the active generation (null:
// none).
type DraftStatus struct {
	LatestVersion int               `json:"latestVersion"`
	Generation    *ActiveGeneration `json:"generation"`
}

// GenerateStarted answers Generate: the job to follow with GenerationStatus.
type GenerateStarted struct {
	JobID string `json:"jobId"`
	Base  int    `json:"base"`
}

// GenerationStatus is the state of a generation: "generating", "waitingNetwork", "waitingQuota", "failed" or "done",
// with a fixed text for the waiting and failed states.
type GenerationStatus struct {
	State   string `json:"state"`
	Message string `json:"message"`
	Percent int    `json:"percent"` // the progress the job reported, 0..100
}

// ExportResult answers ExportDraft and ExportDocx: the version written and the name of the file a send attaches.
type ExportResult struct {
	Version  int    `json:"version"`
	FileName string `json:"fileName"`
}

// DocxSaveResult answers «Сохранить в Word (.docx)»: Status "saved", "cancelled" or "failed", a fixed text and the
// base name of the saved file ("" unless saved).
type DocxSaveResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Name    string `json:"name"`
}

// SendPreparation is the content of the confirmation dialog. Status "ready": the dialog shows recipient, subject and
// attachment and the user may confirm ProposalID; "rejected": it shows Message (Reason is the core's code).
// AttachmentFormat is the format the preparation is for ("txt" or "docx"); ConfirmSend sends it back.
type SendPreparation struct {
	ProposalID       string `json:"proposalId"`
	Status           string `json:"status"`
	Reason           string `json:"reason"`
	Message          string `json:"message"`
	Recipient        string `json:"recipient"`
	Subject          string `json:"subject"`
	AttachmentName   string `json:"attachmentName"`
	AttachmentSize   int64  `json:"attachmentSize"`
	AttachmentFormat string `json:"attachmentFormat"`
}

// DeliveryItem is the JSON form of a delivery (AC-15). Status is the core's code (PENDING, SENDING, SENT, FAILED,
// UNKNOWN); CanMarkSent and CanSendAgain come from the delivery aggregate; Notice explains an unknown outcome.
type DeliveryItem struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Recipient    string `json:"recipient"`
	Subject      string `json:"subject"`
	Attempts     int    `json:"attempts"`
	LastError    string `json:"lastError"`
	ConfirmedAt  string `json:"confirmedAt"`
	SentAt       string `json:"sentAt"`
	CreatedAt    string `json:"createdAt"`
	CanMarkSent  bool   `json:"canMarkSent"`
	CanSendAgain bool   `json:"canSendAgain"`
	Notice       string `json:"notice"`
}

// HomeworkService is bound to the UI: every exported method is callable from the page by its full name
// "<package path>.HomeworkService.<Method>". Keep the method set to what the page may call. Only ConfirmSend leads
// to an email, and only for a proposal the user saw in the dialog; SendAgain resends an unknown one on the user's
// decision.
type HomeworkService struct {
	proposals  HomeworkProposals
	drafts     HomeworkDrafts
	deliveries HomeworkDeliveries
	docx       DocxFilePicker // nil: «Сохранить в Word» fails
	log        *slog.Logger
}

// NewHomeworkService returns the binding of the homework page; OpenServices checks the core before calling it.
func NewHomeworkService(c HomeworkCore, log *slog.Logger) *HomeworkService {
	return &HomeworkService{proposals: c.Proposals, drafts: c.Drafts, deliveries: c.Deliveries, log: log}
}

// withDocxPicker adds the save dialog of «Сохранить в Word (.docx)».
func (s *HomeworkService) withDocxPicker(p DocxFilePicker) *HomeworkService {
	s.docx = p
	return s
}

// ListAssignments returns the newest assignments, newest first.
func (s *HomeworkService) ListAssignments(ctx context.Context) ([]AssignmentItem, error) {
	as, err := s.proposals.ListAssignments(ctx)
	if err != nil {
		s.log.Error(opListAssignments, "err", err)
		return nil, ErrHomeworkUnavailable
	}
	out := make([]AssignmentItem, 0, len(as))
	for _, a := range as {
		out = append(out, assignmentItem(a))
	}
	return out, nil
}

// OpenAssignment returns an assignment with its draft versions and the text of the latest version.
func (s *HomeworkService) OpenAssignment(ctx context.Context, id string) (AssignmentDetail, error) {
	aid, err := assignmentID(id)
	if err != nil {
		return AssignmentDetail{}, err
	}
	a, err := s.proposals.Assignment(ctx, aid)
	if err != nil {
		return AssignmentDetail{}, s.fail(opOpenAssignment, err, ErrHomeworkNotFound)
	}
	versions, err := s.drafts.DraftVersions(ctx, aid)
	if err != nil {
		return AssignmentDetail{}, s.fail(opOpenAssignment, err, ErrHomeworkNotFound)
	}
	out := AssignmentDetail{Assignment: assignmentItem(a), Versions: make([]VersionItem, 0, len(versions))}
	for _, v := range versions {
		out.Versions = append(out.Versions, VersionItem{
			Number: v.Number, Source: string(v.Origin), Instruction: v.Instruction, Latest: v.Latest,
			CreatedAt: rfc3339(v.CreatedAt),
		})
		if v.Latest {
			t := versionText(v)
			out.Latest = &t
		}
	}
	if out.Generation, err = s.activeGeneration(ctx, aid); err != nil {
		return AssignmentDetail{}, s.fail(opOpenAssignment, err, ErrHomeworkNotFound)
	}
	return out, nil
}

// DraftStatus returns the number of the latest draft version and the generation that is pending or running, for the
// assignment page to poll while it is open (TASK-094): a larger number than the page shows means a new version.
func (s *HomeworkService) DraftStatus(ctx context.Context, id string) (DraftStatus, error) {
	aid, err := assignmentID(id)
	if err != nil {
		return DraftStatus{}, err
	}
	versions, err := s.drafts.DraftVersions(ctx, aid)
	if err != nil {
		return DraftStatus{}, s.fail(opDraftStatus, err, ErrHomeworkNotFound)
	}
	var out DraftStatus
	for _, v := range versions {
		out.LatestVersion = max(out.LatestVersion, v.Number)
	}
	if out.Generation, err = s.activeGeneration(ctx, aid); err != nil {
		return DraftStatus{}, s.fail(opDraftStatus, err, ErrHomeworkNotFound)
	}
	return out, nil
}

// activeGeneration is the pending or running generation of a, nil when none.
func (s *HomeworkService) activeGeneration(ctx context.Context, a shared.AssignmentID) (*ActiveGeneration, error) {
	v, ok, err := s.drafts.ActiveGeneration(ctx, a)
	if err != nil || !ok {
		return nil, err
	}
	st := generationStatus(v)
	return &ActiveGeneration{JobID: string(v.JobID), State: st.State, Message: st.Message, Percent: st.Percent}, nil
}

// CorrectLesson stores the subject and teacher the student entered for assignment id; they win over the schedule and
// the hint from the recording. An empty text removes the correction of that field. Returns the assignment as shown.
func (s *HomeworkService) CorrectLesson(ctx context.Context, id, subject, teacher string) (AssignmentItem, error) {
	aid, err := assignmentID(id)
	if err != nil {
		return AssignmentItem{}, err
	}
	v, err := s.proposals.CorrectLesson(ctx, homeworkflow.LessonCorrection{ID: aid, Subject: subject, Teacher: teacher})
	if err != nil {
		return AssignmentItem{}, s.fail(opCorrectLesson, err, ErrHomeworkNotFound)
	}
	return assignmentItem(v), nil
}

// DraftVersion returns version n of the draft of assignment id.
func (s *HomeworkService) DraftVersion(ctx context.Context, id string, n int) (VersionText, error) {
	aid, err := assignmentID(id)
	if err != nil {
		return VersionText{}, err
	}
	if n < 1 {
		return VersionText{}, ErrDraftVersionNotFound
	}
	v, err := s.drafts.DraftVersion(ctx, aid, n)
	if err != nil {
		return VersionText{}, s.fail(opDraftVersion, err, ErrDraftVersionNotFound)
	}
	return versionText(v), nil
}

// Generate starts a new draft version from version base (0: no draft yet), the one the page shows, with the
// student's additional instructions (may be empty).
func (s *HomeworkService) Generate(ctx context.Context, id string, base int, instruction string) (GenerateStarted, error) {
	aid, err := assignmentID(id)
	if err != nil {
		return GenerateStarted{}, err
	}
	res, err := s.drafts.GenerateFrom(ctx, aid, base, instruction)
	switch {
	case err == nil:
		return GenerateStarted{JobID: string(res.JobID), Base: res.Base}, nil
	case errors.Is(err, homeworkdraft.ErrStaleBase):
		return GenerateStarted{}, ErrDraftStale
	case errors.Is(err, values.ErrInvalidDraftRequest):
		return GenerateStarted{}, ErrInstructionInvalid
	case errors.Is(err, homeworkdraft.ErrFull):
		return GenerateStarted{}, ErrDraftFull
	case errors.Is(err, homeworkflow.ErrGenerateRetries):
		s.log.Info(opGenerate, "result", homeworkflow.ErrGenerateRetries)
		return GenerateStarted{}, ErrGenerateRetries
	case errors.Is(err, homeworkflow.ErrGenerationRunning):
		s.log.Info(opGenerate, "result", homeworkflow.ErrGenerationRunning)
		return GenerateStarted{}, ErrGenerationRunning
	}
	return GenerateStarted{}, s.fail(opGenerate, err, ErrHomeworkNotFound)
}

// GenerationStatus returns the state of the generation job Generate started.
func (s *HomeworkService) GenerationStatus(ctx context.Context, jobID string) (GenerationStatus, error) {
	jid := shared.JobID(jobID)
	if jid.Validate() != nil {
		return GenerationStatus{}, ErrHomeworkInvalidID
	}
	v, err := s.drafts.Generation(ctx, jid)
	if err != nil {
		return GenerationStatus{}, s.fail(opGeneration, err, ErrGenerationNotFound)
	}
	return generationStatus(v), nil
}

// generationStatus is the page's state of a generation job.
func generationStatus(v homeworkflow.GenerationView) GenerationStatus {
	switch v.Status {
	case job.StatusCompleted:
		return GenerationStatus{State: genDone, Percent: 100}
	case job.StatusWaitingForNetwork:
		return GenerationStatus{State: genWaitingNetwork, Message: msgWaitingNetwork, Percent: v.Percent}
	case job.StatusWaitingForQuota:
		return GenerationStatus{State: genWaitingQuota, Message: msgWaitingQuota, Percent: v.Percent}
	case job.StatusCancelled:
		return GenerationStatus{State: genFailed, Message: msgGenerationCancel}
	case job.StatusFailed:
		for _, m := range generationMessages {
			if errors.Is(v.Failure, m.err) {
				return GenerationStatus{State: genFailed, Message: m.msg}
			}
		}
		return GenerationStatus{State: genFailed, Message: msgGenerationFailed}
	}
	return GenerationStatus{State: genGenerating, Percent: v.Percent}
}

// EditDraft stores what the student wrote over version base as a new version and returns the latest version.
func (s *HomeworkService) EditDraft(ctx context.Context, id string, base int, title, body string) (VersionText, error) {
	aid, err := assignmentID(id)
	if err != nil {
		return VersionText{}, err
	}
	v, err := s.drafts.EditDraft(ctx, homeworkflow.DraftEdit{Assignment: aid, Base: base, Title: title, Body: body})
	switch {
	case err == nil:
		return versionText(v), nil
	case errors.Is(err, homeworkdraft.ErrStaleBase):
		return VersionText{}, ErrDraftStale
	case errors.Is(err, homeworkdraft.ErrInvalidContent):
		return VersionText{}, ErrDraftInvalid
	case errors.Is(err, homeworkdraft.ErrFull):
		return VersionText{}, ErrDraftFull
	}
	return VersionText{}, s.fail(opEditDraft, err, ErrHomeworkNotFound)
}

// ExportDraft writes version n to the file a send attaches, replacing an earlier export. While an email of the
// assignment is unresolved, nothing is written (ErrExportLocked).
func (s *HomeworkService) ExportDraft(ctx context.Context, id string, n int) (ExportResult, error) {
	aid, err := assignmentID(id)
	if err != nil {
		return ExportResult{}, err
	}
	if n < 1 {
		return ExportResult{}, ErrDraftVersionNotFound
	}
	res, err := s.drafts.ExportDraft(ctx, aid, n)
	if errors.Is(err, homeworkflow.ErrExportLocked) {
		s.log.Info(opExportDraft, "result", homeworkflow.ErrExportLocked)
		return ExportResult{}, ErrExportLocked
	}
	if err != nil {
		return ExportResult{}, s.fail(opExportDraft, err, ErrDraftVersionNotFound)
	}
	return ExportResult{Version: res.Version, FileName: path.Base(res.File.Path())}, nil
}

// ExportDocx writes version n as a Word file with the title page (TASK-094) to the file a send in .docx attaches,
// replacing an earlier one. While an email of the assignment is unresolved, nothing is written (ErrExportLocked).
func (s *HomeworkService) ExportDocx(ctx context.Context, id string, n int) (ExportResult, error) {
	aid, err := assignmentID(id)
	if err != nil {
		return ExportResult{}, err
	}
	if n < 1 {
		return ExportResult{}, ErrDraftVersionNotFound
	}
	res, err := s.drafts.ExportDocx(ctx, aid, n)
	if errors.Is(err, homeworkflow.ErrExportLocked) {
		s.log.Info(opExportDocx, "result", homeworkflow.ErrExportLocked)
		return ExportResult{}, ErrExportLocked
	}
	if err != nil {
		return ExportResult{}, s.fail(opExportDocx, err, ErrDraftVersionNotFound)
	}
	return ExportResult{Version: res.Version, FileName: path.Base(res.File.Path())}, nil
}

// SaveDocx is «Сохранить в Word (.docx)»: it asks in the native save dialog where to save version n as a Word file
// with the title page (the profile and the lesson of the assignment) and writes it there. It does not touch the file a
// send attaches: the page calls ExportDocx before the send dialog. Neither the path nor an error text reaches the page;
// the page gets the base name of the file.
func (s *HomeworkService) SaveDocx(ctx context.Context, id string, n int) (DocxSaveResult, error) {
	aid, err := assignmentID(id)
	if err != nil {
		return DocxSaveResult{}, err
	}
	if n < 1 {
		return DocxSaveResult{}, ErrDraftVersionNotFound
	}
	// Checked before the dialog: a missing version is an error of the page, not an empty file.
	if _, err := s.drafts.DraftVersion(ctx, aid, n); err != nil {
		return DocxSaveResult{}, s.fail(opSaveDocx, err, ErrDraftVersionNotFound)
	}
	name, err := s.drafts.DocxName(ctx, aid)
	if err != nil {
		return DocxSaveResult{}, s.fail(opSaveDocx, err, ErrHomeworkNotFound)
	}
	if s.docx == nil {
		s.log.Error(opPickDocx, "err", "no save dialog")
		return DocxSaveResult{Status: docxFailed, Message: msgDocxFailed}, nil
	}
	target, err := s.docx.PickDocxFile(ctx, name)
	switch {
	case err != nil:
		s.log.Warn(opPickDocx, "err", err)
		return DocxSaveResult{Status: docxFailed, Message: msgDocxFailed}, nil
	case target == "":
		return DocxSaveResult{Status: docxCancelled, Message: msgDocxCancelled}, nil
	}
	if err := s.drafts.SaveDocx(ctx, aid, n, target); err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			return DocxSaveResult{}, s.fail(opSaveDocx, err, ErrDraftVersionNotFound)
		}
		s.log.Error(opSaveDocx, "err", err) // the adapters' errors carry neither the path nor the text
		return DocxSaveResult{Status: docxFailed, Message: msgDocxFailed}, nil
	}
	return DocxSaveResult{Status: docxSaved, Message: msgDocxSaved, Name: baseName(target)}, nil
}

// PrepareSend checks a send of the exported homework with the attachment in format ("txt" or "docx") against the
// trusted local state and returns the content of the confirmation dialog. A change of the format in the dialog is a
// new PrepareSend. Nothing is sent: only ConfirmSend sends.
func (s *HomeworkService) PrepareSend(ctx context.Context, id, format string) (SendPreparation, error) {
	aid, err := assignmentID(id)
	if err != nil {
		return SendPreparation{}, err
	}
	f, err := sendFormat(format)
	if err != nil {
		return SendPreparation{}, err
	}
	v, err := s.proposals.ProposeSend(ctx, aid, f)
	if err != nil {
		return SendPreparation{}, s.fail(opPrepareSend, err, ErrHomeworkNotFound)
	}
	out := SendPreparation{ProposalID: string(v.ID), Reason: string(v.Reason), AttachmentFormat: format}
	if v.Status != proposal.StatusValidated {
		out.Status, out.Message = sendRejected, msgRejectedDefault
		if msg, ok := rejectMessages[v.Reason]; ok {
			out.Message = msg
		}
		if v.Reason == proposal.ReasonNoArtifact && f == proposal.FormatDocx {
			out.Message = msgNoDocx
		}
		s.log.Info(opPrepareSend, "result", v.Status, "reason", v.Reason)
		return out, nil
	}
	out.Status = sendReady
	out.Recipient, out.Subject = v.Recipient, v.Subject
	if out.Subject == "" {
		out.Subject = s.deliveries.Subject()
	}
	out.AttachmentName, out.AttachmentSize = v.AttachmentName, v.AttachmentSize
	return out, nil
}

// ConfirmSend is the user's click on «Отправить» in the confirmation dialog (AC-14): the use case confirms the send
// proposal the dialog showed and executes it, which creates the delivery. It is the only binding that leads to an
// email. subject and format are the ones the dialog showed (SendPreparation.Subject and AttachmentFormat): if the
// core's subject changed meanwhile or the format is not the prepared one, nothing is sent (ErrSendStale). Repeating it
// with the same proposal after a failure creates no second email (ErrSendIncomplete says so).
func (s *HomeworkService) ConfirmSend(ctx context.Context, proposalID, subject, format string) error {
	pid := proposal.ID(proposalID)
	if pid.Validate() != nil {
		return ErrHomeworkInvalidID
	}
	f, err := sendFormat(format)
	if err != nil {
		return err
	}
	_, err = s.proposals.ConfirmSend(ctx, pid, subject, f)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, homeworkflow.ErrNotASend):
		s.log.Warn(opConfirmSend, "result", err)
		return ErrSendNotFound
	case errors.Is(err, shared.ErrNotFound):
		s.log.Info(opConfirmSend, "result", "not found")
		return ErrSendNotFound
	case errors.Is(err, proposal.ErrInvalidTransition):
		s.log.Info(opConfirmSend, "result", err)
		return ErrSendCannotConfirm
	case errors.Is(err, proposal.ErrStale):
		s.log.Info(opConfirmSend, "result", err)
		return ErrSendStale
	case errors.Is(err, senddelivery.ErrUnresolvedDelivery):
		s.log.Info(opConfirmSend, "result", err)
		return ErrSendUnresolved
	}
	s.log.Error(opConfirmSend, "err", err)
	return ErrSendIncomplete
}

// CancelSend is «Отмена» in the confirmation dialog: the proposal is declined and can never be sent.
func (s *HomeworkService) CancelSend(ctx context.Context, proposalID string) error {
	pid, err := s.sendProposal(ctx, opCancelSend, proposalID)
	if err != nil {
		return err
	}
	if _, err := s.proposals.Decline(ctx, pid); err != nil {
		if errors.Is(err, proposal.ErrInvalidTransition) {
			return ErrSendAlreadyDecided
		}
		return s.fail(opCancelSend, err, ErrSendNotFound)
	}
	return nil
}

// sendProposal checks that id names a send proposal: the dialog's buttons act on nothing else.
func (s *HomeworkService) sendProposal(ctx context.Context, op, id string) (proposal.ID, error) {
	pid := proposal.ID(id)
	if pid.Validate() != nil {
		return "", ErrHomeworkInvalidID
	}
	v, err := s.proposals.Get(ctx, pid)
	if err != nil {
		return "", s.fail(op, err, ErrSendNotFound)
	}
	if v.Kind != proposal.KindSendHomework {
		s.log.Warn(op, "result", "not a send proposal", "kind", v.Kind)
		return "", ErrSendNotFound
	}
	return pid, nil
}

// Deliveries returns the emails of assignment id, newest first.
func (s *HomeworkService) Deliveries(ctx context.Context, id string) ([]DeliveryItem, error) {
	aid, err := assignmentID(id)
	if err != nil {
		return nil, err
	}
	vs, err := s.deliveries.ListForAssignment(ctx, aid)
	if err != nil {
		s.log.Error(opDeliveries, "err", err)
		return nil, ErrHomeworkUnavailable
	}
	out := make([]DeliveryItem, 0, len(vs))
	for _, v := range vs {
		out = append(out, deliveryItem(v))
	}
	return out, nil
}

// MarkSent records that the student found an email with an unknown outcome in the mailbox.
func (s *HomeworkService) MarkSent(ctx context.Context, deliveryID string) (DeliveryItem, error) {
	return s.resolve(ctx, opMarkSent, deliveryID, s.deliveries.MarkSent)
}

// SendAgain sends an email with an unknown outcome again; the page calls it only from its own confirmation dialog,
// because the teacher may get the email twice.
func (s *HomeworkService) SendAgain(ctx context.Context, deliveryID string) (DeliveryItem, error) {
	return s.resolve(ctx, opSendAgain, deliveryID, s.deliveries.SendAgain)
}

func (s *HomeworkService) resolve(ctx context.Context, op, id string,
	apply func(context.Context, shared.DeliveryID) (senddelivery.View, error)) (DeliveryItem, error) {
	did := shared.DeliveryID(id)
	if did.Validate() != nil {
		return DeliveryItem{}, ErrHomeworkInvalidID
	}
	v, err := apply(ctx, did)
	if errors.Is(err, delivery.ErrInvalidTransition) {
		s.log.Info(op, "result", err)
		return DeliveryItem{}, ErrDeliveryActionInvalid
	}
	if err != nil {
		return DeliveryItem{}, s.fail(op, err, ErrDeliveryNotFound)
	}
	return deliveryItem(v), nil
}

// fail maps a missing item to notFound and anything else to ErrHomeworkUnavailable; the cause goes to the log.
func (s *HomeworkService) fail(op string, err, notFound error) error {
	if errors.Is(err, shared.ErrNotFound) {
		s.log.Info(op, "result", "not found")
		return notFound
	}
	s.log.Error(op, "err", err)
	return ErrHomeworkUnavailable
}

// sendFormat reads the attachment format of the page.
func sendFormat(format string) (proposal.Format, error) {
	switch format {
	case formatTxt:
		return proposal.FormatText, nil
	case formatDocx:
		return proposal.FormatDocx, nil
	}
	return "", ErrSendFormat
}

func assignmentID(id string) (shared.AssignmentID, error) {
	aid := shared.AssignmentID(id)
	if aid.Validate() != nil {
		return "", ErrHomeworkInvalidID
	}
	return aid, nil
}

func assignmentItem(a homeworkflow.AssignmentView) AssignmentItem {
	return AssignmentItem{
		ID: string(a.ID), LessonID: string(a.LessonID), Title: a.Title, Description: a.Description, Due: rfc3339(a.Due),
		UserEdited: a.UserEdited, UpdatedAt: rfc3339(a.UpdatedAt),
		Subject: a.Subject, SubjectSource: string(a.SubjectSource), Kind: a.Kind, Teacher: a.Teacher,
		TeacherSource: string(a.TeacherSource), LessonDate: rfc3339(a.LessonDate),
		CorrectedSubject: a.CorrectedSubject, CorrectedTeacher: a.CorrectedTeacher,
	}
}

func versionText(v homeworkflow.DraftVersionView) VersionText {
	return VersionText{
		Number: v.Number, Source: string(v.Origin), Title: v.Title, Body: v.Body, Instruction: v.Instruction,
		Latest: v.Latest, CreatedAt: rfc3339(v.CreatedAt),
	}
}

func deliveryItem(v senddelivery.View) DeliveryItem {
	out := DeliveryItem{
		ID: string(v.ID), Status: string(v.Status), Recipient: v.Recipient, Subject: v.Subject, Attempts: v.Attempts,
		LastError: v.LastError, ConfirmedAt: rfc3339(v.ConfirmedAt), SentAt: rfc3339(v.SentAt),
		CreatedAt: rfc3339(v.CreatedAt), CanMarkSent: v.CanMarkSent, CanSendAgain: v.CanSendAgain,
	}
	if v.OutcomeUnknown {
		out.Notice = msgUnknownOutcome
	}
	return out
}
