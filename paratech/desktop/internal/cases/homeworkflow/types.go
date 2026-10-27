package homeworkflow

import (
	"log/slog"
	"path"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// Job types of the post-processing of a transcript (PostProcessor). The app registers both with the job runner as
// light jobs (not Heavy: they wait on the Hub, not on the CPU) with the requeue recovery policy: the LLM call is
// idempotent by its key and the local writes are repeatable, and nothing leaves the machine but the request.
const (
	SummarizeJobType       = job.Type(values.SummarizeJobType)
	ExtractHomeworkJobType = job.Type(values.ExtractHomeworkJobType)
	LectureTextJobType     = job.Type(values.LectureTextJobType) // the lecture text, part by part (TASK-090)
)

// HomeworkDraftJobType is the job type of a draft generation (Drafts). The app registers it like the post-processing
// types: light, with the requeue recovery policy (the LLM call is idempotent by its key and a version is added at most
// once per job).
const HomeworkDraftJobType = job.Type(values.HomeworkDraftJobType)

// DefaultPostProcessAttempts bounds the attempts of a post-processing job when PostProcessDeps.MaxAttempts is 0.
// Waiting for the network or the quota does not use attempts.
const DefaultPostProcessAttempts = 5

// MaxListedAssignments bounds the assignment list of the homework screen (Service.ListAssignments).
const MaxListedAssignments = 200

// saveAttempts bounds the reload-and-decide-again loop of one save that keeps meeting conflicts.
const saveAttempts = 3

// answeredPercent is the progress reported once the model answer passed the checks.
const answeredPercent = 50

// Deps are the ports the Service needs; all are required.
type Deps struct {
	Proposals      ProposalRepository
	Assignments    AssignmentRepository
	AssignmentList AssignmentList
	Artifacts      ArtifactStore
	Recipients     RecipientSource
	Mailer         HomeworkMailer
	Exporter       HomeworkExporter
	Calendar       DeadlineCalendar
	Recordings     TranscriptRecordings // the lesson snapshot of an assignment (TASK-092)
	Lessons        LessonReader         // the live lesson when there is no snapshot
	Clock          Clock
	IDs            IDGenerator
	Tx             Tx

	// Optional (TASK-127): the agent's send of a named draft version and the version shown on its confirmation, and
	// the reuse of a deadline or version send proposal that already waits for «Да».
	Drafts  DraftReader
	Waiting WaitingProposals

	// Optional (TASK-180): the send of a turnkey work's file to the teacher.
	Works WorkFiles

	// Optional (TASK-215): the subject of an assignment the student adds; without it a subject is ErrInvalidConfig.
	Subjects SubjectNames

	// Optional (TASK-229): DeleteAssignment queues the removal of the Google deadline event and asks for a sync,
	// RestoreAssignment forgets a removal still waiting. Without Removals the event stays in Google.
	Removals DeadlineRemovals
	Sync     CalendarSyncRequester
}

// ProposalView is a proposal as the confirmation dialog shows it (PRD section 13.3, AC-14): what will happen, to which
// assignment, with which data and to which trusted target. Title and Body are model text: show them as data.
type ProposalView struct {
	ID           proposal.ID
	Kind         proposal.Kind
	Status       proposal.Status
	Reason       proposal.RejectReason // set when REJECTED
	AssignmentID shared.AssignmentID
	Title        string
	Body         string
	Due          time.Time
	Recipient    string // the trusted address for send_homework_email, set once validated
	Subject      string // the email subject of a validated send_homework_email, built by the core (TASK-092)
	CreatedAt    time.Time
	ConfirmedAt  time.Time

	// The attachment of send_homework_email pinned at validation: the file name of proposal.ExportRefOf and the size of
	// the pinned content. Empty and 0 while nothing is pinned. AttachmentFormat is the format the user chose (TASK-094).
	AttachmentName   string
	AttachmentSize   int64
	AttachmentFormat proposal.Format
	// DraftVersion is the draft version whose text is exactly the pinned attachment (a text export); 0 when none is
	// (a Word file, an edit since the export) or the drafts are not wired (TASK-127).
	DraftVersion int
	// WorkID is the work whose file a send_work_email sends (TASK-180); its AttachmentName is the name the copy and the
	// email give the file.
	WorkID shared.WorkID
}

func viewOf(p *proposal.Proposal) ProposalView {
	v := ProposalView{
		ID: p.ID(), Kind: p.Kind(), Status: p.Status(), Reason: p.Reason(), AssignmentID: p.AssignmentID(),
		Title: p.Title(), Body: p.Body(), Due: p.Due(), Recipient: p.Recipient().Address(),
		CreatedAt: p.CreatedAt(), ConfirmedAt: p.ConfirmedAt(), AttachmentFormat: p.AttachmentFormat(), WorkID: p.WorkID(),
	}
	if p.Kind() == proposal.KindSendWork {
		v.AttachmentSize = p.AttachmentDigest().Size() // the name is the library's (Service.proposalView)
		return v
	}
	if d := p.AttachmentDigest(); !d.IsZero() {
		// The ref of a stored proposal's assignment is valid: the id was checked when it was parsed.
		if ref, err := proposal.ExportRefOf(p.AssignmentID(), p.AttachmentFormat()); err == nil {
			v.AttachmentName, v.AttachmentSize = path.Base(ref.Path()), d.Size()
		}
	}
	return v
}

// PostProcessDeps are the ports and settings of the PostProcessor. Log is optional.
type PostProcessDeps struct {
	LLM          LLMClient
	Transcripts  TranscriptReader
	Recordings   RecordingReader
	Lessons      LessonReader
	Artifacts    ArtifactStore
	Summaries    SummaryRepository
	Assignments  AssignmentRepository
	LectureTexts LectureTextRepository
	Jobs         JobEnqueuer
	Clock        Clock
	IDs          IDGenerator
	Log          *slog.Logger

	// Materials and MaterialAssignments run the jobs on a library material (TASK-122); both or neither. Without
	// them a material job fails as an unknown job type.
	Materials           MaterialTexts
	MaterialAssignments MaterialAssignments

	// ModelAlias is the Hub model alias of the requests, from the app configuration (values.NewLLMRequest rules).
	ModelAlias string

	// MaxAttempts of each job; 0 means DefaultPostProcessAttempts.
	MaxAttempts int
}

// AssignmentEdit is what the user entered for an assignment (PRD section 10: review and edit locally, AC-13). A zero
// Due removes the deadline.
type AssignmentEdit struct {
	ID          shared.AssignmentID
	Title       string
	Description string
	Due         time.Time
}

// NewAssignment is an assignment or a reminder the student adds by hand or through the agent (ADR-0029). The texts
// are the student's words: data, never instructions.
type NewAssignment struct {
	// ID is the caller's idempotency key (a hash of the agent call or of the form's request token): the same ID with
	// the same content returns the stored assignment. Empty: a new ID.
	ID      shared.AssignmentID
	Title   string
	Note    string           // optional; the description
	Subject shared.SubjectID // optional: a library subject; it wins over the lesson's
	Lesson  shared.LessonID  // optional: a lesson of the schedule
	Date    string           // optional: YYYY-MM-DD in the student's zone
	Time    string           // optional: HH:MM, 24 h, only with Date; without it 23:59
}

// AssignmentView is an assignment as the UI shows it. Title and Description may be model text: show them as data.
type AssignmentView struct {
	ID          shared.AssignmentID
	LessonID    shared.LessonID
	Title       string
	Description string
	Due         time.Time // zero: no deadline
	UserEdited  bool
	Version     int64 // the stored version, for the UI to tell a changed assignment
	UpdatedAt   time.Time

	// The lesson (TASK-092): «Предмет · тип · преподаватель», «занятие 19.10». A subject or teacher from the
	// transcript (values.SourceTranscript) is model output and is shown marked «по записи».
	Subject       string
	SubjectSource values.NameSource
	Kind          string // code of the lesson kind ("lecture", ...); empty if unknown
	Teacher       string
	TeacherSource values.NameSource
	LessonDate    time.Time // the lesson start in the student's zone; zero if unknown
	// CorrectedSubject and CorrectedTeacher are the user's correction, for the edit form; empty: none.
	CorrectedSubject string
	CorrectedTeacher string
	// MaterialID is the library material the assignment was found in; empty for one from a lesson (TASK-122).
	MaterialID shared.MaterialID
	// Deleted is set only in the view DeleteAssignment returns, for «Задание удалено» with «Вернуть» (ADR-0029); other
	// readers never show a deleted assignment.
	Deleted bool
}

func assignmentViewOf(a *assignment.Assignment, l values.LessonLabel) AssignmentView {
	d := a.Details()
	return AssignmentView{
		ID: a.ID(), LessonID: a.LessonID(), Title: d.Title(), Description: d.Description(), Due: d.Due(),
		UserEdited: a.UserEdited(), Version: a.Version(), UpdatedAt: a.UpdatedAt(),
		Subject: l.Subject(), SubjectSource: l.SubjectSource(), Kind: l.Kind(), Teacher: l.Teacher(),
		TeacherSource: l.TeacherSource(), LessonDate: l.Date(),
		CorrectedSubject: a.Correction().Subject(), CorrectedTeacher: a.Correction().Teacher(), MaterialID: a.MaterialID(),
		Deleted: a.Deleted(),
	}
}

// LessonCorrection is the subject and teacher the user entered for an assignment (TASK-092); an empty field removes
// the correction of that field.
type LessonCorrection struct {
	ID      shared.AssignmentID
	Subject string
	Teacher string
}

// DraftDeps are the ports and settings of Drafts. Log is optional.
type DraftDeps struct {
	LLM         LLMClient
	Assignments AssignmentRepository
	Recordings  TranscriptRecordings // the lesson snapshot of the assignment (TASK-092)
	Drafts      DraftRepository
	Summaries   SummaryRepository
	Lessons     LessonReader
	Artifacts   ArtifactStore
	Jobs        JobEnqueuer
	JobReader   JobReader
	Deliveries  UnresolvedDeliveries
	Profiles    ProfileSource    // the student profile of the title page (TASK-094)
	Renderer    DocumentRenderer // the Word file of a version
	Files       DocumentWriter   // a Word file saved where the student chose
	Clock       Clock
	IDs         IDGenerator
	Tx          Tx
	Log         *slog.Logger

	// ModelAlias is the Hub model alias of the requests, from the app configuration (values.NewLLMRequest rules).
	ModelAlias string

	// MaxAttempts of each generation job; 0 means DefaultPostProcessAttempts.
	MaxAttempts int

	// GenerateRetries bounds how often Generate starts the same generation again after its job failed or was
	// cancelled, then it returns ErrGenerateRetries; 0 means DefaultGenerateRetries, at most MaxGenerateRetries.
	GenerateRetries int
}

// Bounds of DraftDeps.GenerateRetries. The maximum keeps the round suffix of the job key short.
const (
	DefaultGenerateRetries = 20
	MaxGenerateRetries     = 1000
)

// GenerateResult is the generation job Generate started or found. Created is false when an equivalent generation
// (same assignment, base version and instructions) already had a live or completed job, for example after a double
// click.
type GenerateResult struct {
	JobID   shared.JobID
	Created bool
	Base    int // the draft version the generation starts from; 0: no draft yet
}

// DraftEdit is what the student wrote for a draft (PRD section 10 step 3). Base is the version shown while editing.
type DraftEdit struct {
	Assignment shared.AssignmentID
	Base       int
	Title      string
	Body       string
}

// DraftVersionView is a draft version as the UI shows it. Title, Body and Instruction may be model or student text:
// show them as data.
type DraftVersionView struct {
	Assignment    shared.AssignmentID
	Number        int
	Origin        homeworkdraft.Origin
	Title         string
	Body          string
	Instruction   string // generated versions: the instructions used
	PromptVersion int    // generated versions only
	Latest        bool
	CreatedAt     time.Time
}

func draftVersionViewOf(d *homeworkdraft.Draft, r homeworkdraft.Revision) DraftVersionView {
	return DraftVersionView{
		Assignment: d.Assignment(), Number: r.Number(), Origin: r.Origin(), Title: r.Title(), Body: r.Body(),
		Instruction: r.Instruction(), PromptVersion: r.PromptVersion(), Latest: r.Number() == d.LatestNumber(),
		CreatedAt: r.CreatedAt(),
	}
}

// GenerationView is the state of a generation job for the homework screen (Drafts.Generation, Drafts.ActiveGeneration).
type GenerationView struct {
	JobID   shared.JobID
	Status  job.Status
	Percent int // the progress the job reported, 0..100
	// Failure is the ErrDraft* error a FAILED job ended with (errors.Is); nil for other statuses or an unknown cause.
	Failure error
}

// DraftExport tells which version was exported to which file.
type DraftExport struct {
	Version int
	File    shared.ArtifactRef // proposal.ExportRef of the assignment, the file a send attaches
}
