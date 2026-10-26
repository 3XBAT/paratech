package turnkey

import (
	"log/slog"
	"slices"
	"time"

	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports of the service; every one except Advisor, Writing and Log is required.
type Deps struct {
	Works       WorkRepository
	Assignments Assignments
	Materials   MaterialTexts
	Reader      Reader
	Advisor     StepAdvisor  // nil: AskStep answers ErrNotConfigured
	Writing     *WritingDeps // nil: the writing answers ErrNotConfigured
	Clock       Clock
	IDs         IDs
	Log         *slog.Logger
}

// WritingDeps are the ports of the writing (TASK-131); every port is required but Slides, PDF and Canceller.
type WritingDeps struct {
	Writer    Writer
	Jobs      Jobs
	JobIDs    JobIDs
	Canceller JobCanceller // nil: StopWriting answers ErrNotConfigured (TASK-178)
	Profiles  Profiles
	Renderer  Renderer
	Slides    SlideRenderer // nil: a presentation is not written (ErrNotWritable)
	PDF       PaperPDF      // nil: ExportPDF answers ErrNotConfigured (TASK-198)
	Lessons   LessonLabels
	Shelf     Shelf

	ModelAlias  string // the Hub alias of the writing calls
	MaxAttempts int    // attempts of a job; 0: DefaultJobAttempts
}

// WritingStarted is the outcome of StartWriting and Regenerate: the work and its job; Created is false when a job of
// the same writing runs (or ran to its end) already.
type WritingStarted struct {
	View    View
	Job     shared.JobID
	Created bool
}

// PDFSaved is the outcome of ExportPDF: the work and its PDF in the library.
type PDFSaved struct {
	View     View
	Material shared.MaterialID
}

// WritingView is the writing of a work as the progress card, the result card and the preview show it. Every text was
// written by the model and is shown as plain text only.
type WritingView struct {
	Step       work.StepKind // StepApprove: the outline waits for «Утвердить план»
	Generation int           // grows each time a done work is written again
	Topic      string
	Sections   []SectionView
	Done       int // «Раздел N из M»: the written sections
	Total      int
	Subject    shared.SubjectID   // the library subject whose «ДЗ» gets the file; "" «Входящие»
	Material   shared.MaterialID  // the file in the library (the .pptx of a presentation), once done
	Result     shared.ArtifactRef // the ResultCard of the file, once done
	PDF        shared.MaterialID  // the PDF of the done paper next to it (ExportPDF, TASK-198); "" none yet

	// The writing job that runs for the work, read with the view (TASK-178): none once it ended. A stopped running
	// step stays active (RUNNING) until it returns, so the UI keeps polling until Active is false.
	Active   bool
	Job      shared.JobID
	JobState job.Status // QUEUED, RUNNING, RETRY_WAIT or a WAITING_FOR_* state while Active

	// A presentation (TASK-106): its template, the likely questions and the speaker text .docx once done.
	Presentation bool
	Template     cvalues.PresentationTemplate
	Questions    []work.TalkQuestion
	Talk         shared.MaterialID
	TalkResult   shared.ArtifactRef

	// A lab report (TASK-107): what became of the attached manual (ManualNotRead, ManualNoText: the student is told
	// it was not read) and «Что доделать самому», the placeholders of the written sections.
	Lab          bool
	Manual       work.Manual
	Placeholders []work.Placeholder
}

// SectionView is a section of the outline with its text, when written for its current revision. A slide has its Kind,
// its speaker notes in Paragraphs and its Bullets or Columns; a section of a lab report its Part, and its placeholders
// among the Paragraphs («▢ Вставьте …», work.PromptOf).
type SectionView struct {
	Number     int
	Kind       cvalues.SlideKind
	Part       work.LabPart
	Title      string
	Words      int
	Note       string
	Written    bool
	Paragraphs []string
	Sources    []work.Reference
	Bullets    []string
	Columns    []work.Column
}

// View is a work as the cards and the helper page show it (TASK-130). Every text the reader wrote is untrusted and
// shown as plain text only.
type View struct {
	ID      shared.WorkID
	Source  work.Source
	Status  work.Status
	Round   int // the round of the pending or last reader call, 1..3
	Attempt int // the attempt of the pending reader call

	// A work of a topic (Source.Topic, TASK-187) has no assignment, its text untrusted: it is written and exported,
	// never sent to a teacher. Sendable is set for a work of an assignment only (work.Sendable).
	Sendable bool

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

	Answered []Answered   // the questions of the earlier rounds with the student's answers
	Writing  *WritingView // from the start of the writing on (TASK-131)

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
		Partial: w.Partial(), Writing: writingViewOf(w), Version: w.Version(), UpdatedAt: w.UpdatedAt(),
	}
	v.Sendable = w.Sendable() == nil
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

// writingViewOf builds the view of the writing; nil before it starts.
func writingViewOf(w *work.Work) *WritingView {
	wr, ok := w.Writing()
	if !ok {
		return nil
	}
	v := &WritingView{Step: w.Next().Kind, Generation: wr.Generation, Topic: wr.Topic, Subject: wr.Snapshot.Subject}
	v.Done, v.Total = w.Progress()
	for _, s := range wr.Sections {
		sv := SectionView{Number: s.Number, Kind: s.Kind, Part: s.Part, Title: s.Title, Words: s.Words, Note: s.Note, Written: s.Written()}
		if sv.Written {
			sv.Paragraphs, sv.Sources = slices.Clone(s.Text.Paragraphs), slices.Clone(s.Text.Sources)
			sv.Bullets, sv.Columns = slices.Clone(s.Text.Bullets), slices.Clone(s.Text.Columns)
		}
		v.Sections = append(v.Sections, sv)
	}
	done := w.Status() == work.StatusDone
	if f := wr.File; f != nil && done {
		v.Material, v.Result = f.Material, f.ArtifactRef()
	}
	if p, ok := w.PDF(); ok {
		v.PDF = p.Material
	}
	if d := wr.Deck; d != nil {
		v.Presentation, v.Template, v.Questions = true, d.Template, w.TalkQuestions()
		if d.Talk != nil && done {
			v.Talk, v.TalkResult = d.Talk.Material, d.Talk.ArtifactRef()
		}
	}
	if w.LabReport() {
		v.Lab, v.Manual, v.Placeholders = true, w.Manual(), w.Placeholders()
	}
	return v
}
