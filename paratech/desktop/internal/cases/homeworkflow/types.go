package homeworkflow

import (
	"log/slog"
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
)

// HomeworkDraftJobType is the job type of a draft generation (Drafts). The app registers it like the post-processing
// types: light, with the requeue recovery policy (the LLM call is idempotent by its key and a version is added at most
// once per job).
const HomeworkDraftJobType = job.Type(values.HomeworkDraftJobType)

// DefaultPostProcessAttempts bounds the attempts of a post-processing job when PostProcessDeps.MaxAttempts is 0.
// Waiting for the network or the quota does not use attempts.
const DefaultPostProcessAttempts = 5

// saveAttempts bounds the reload-and-decide-again loop of one save that keeps meeting conflicts.
const saveAttempts = 3

// answeredPercent is the progress reported once the model answer passed the checks.
const answeredPercent = 50

// Deps are the ports the Service needs; all are required.
type Deps struct {
	Proposals   ProposalRepository
	Assignments AssignmentRepository
	Artifacts   ArtifactStore
	Recipients  RecipientSource
	Mailer      HomeworkMailer
	Exporter    HomeworkExporter
	Calendar    DeadlineCalendar
	Clock       Clock
	IDs         IDGenerator
	Tx          Tx
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
	CreatedAt    time.Time
	ConfirmedAt  time.Time
}

func viewOf(p *proposal.Proposal) ProposalView {
	return ProposalView{
		ID: p.ID(), Kind: p.Kind(), Status: p.Status(), Reason: p.Reason(), AssignmentID: p.AssignmentID(),
		Title: p.Title(), Body: p.Body(), Due: p.Due(), Recipient: p.Recipient().Address(),
		CreatedAt: p.CreatedAt(), ConfirmedAt: p.ConfirmedAt(),
	}
}

// PostProcessDeps are the ports and settings of the PostProcessor. Log is optional.
type PostProcessDeps struct {
	LLM         LLMClient
	Transcripts TranscriptReader
	Recordings  RecordingReader
	Lessons     LessonReader
	Artifacts   ArtifactStore
	Summaries   SummaryRepository
	Assignments AssignmentRepository
	Jobs        JobEnqueuer
	Clock       Clock
	IDs         IDGenerator
	Log         *slog.Logger

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
}

func assignmentViewOf(a *assignment.Assignment) AssignmentView {
	d := a.Details()
	return AssignmentView{
		ID: a.ID(), LessonID: a.LessonID(), Title: d.Title(), Description: d.Description(), Due: d.Due(),
		UserEdited: a.UserEdited(), Version: a.Version(), UpdatedAt: a.UpdatedAt(),
	}
}

// DraftDeps are the ports and settings of Drafts. Log is optional.
type DraftDeps struct {
	LLM         LLMClient
	Assignments AssignmentRepository
	Drafts      DraftRepository
	Summaries   SummaryRepository
	Lessons     LessonReader
	Artifacts   ArtifactStore
	Jobs        JobEnqueuer
	Clock       Clock
	IDs         IDGenerator
	Tx          Tx
	Log         *slog.Logger

	// ModelAlias is the Hub model alias of the requests, from the app configuration (values.NewLLMRequest rules).
	ModelAlias string

	// MaxAttempts of each generation job; 0 means DefaultPostProcessAttempts.
	MaxAttempts int
}

// GenerateResult is the generation job Generate started or found. Created is false when an equivalent generation
// (same assignment, base version and instructions) already had a job, for example after a double click.
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

// DraftExport tells which version was exported to which file.
type DraftExport struct {
	Version int
	File    shared.ArtifactRef // proposal.ExportRef of the assignment, the file a send attaches
}
