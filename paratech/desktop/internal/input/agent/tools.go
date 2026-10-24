package agent

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// SubjectLibrary is the library of subjects (cases/library) as the read tools use it.
type SubjectLibrary interface {
	Subjects(ctx context.Context) ([]*subject.Subject, error)
	ResolveSubjects(ctx context.Context, names []string) (map[string]subjectmatch.Match, error)
}

// Knowledge is the knowledge base (cases/knowledgebase) as the read tools use it.
type Knowledge interface {
	Subjects(ctx context.Context) ([]kb.Subject, error)
	Lessons(ctx context.Context, key string) ([]kb.Lesson, error)
	Search(ctx context.Context, query string) (knowledgebase.SearchResult, error)
}

// Activity is the recordings and jobs reads (cases/activity): the schedule entry of a recording and the state of jobs
// of every type, since the agent asks about the chain jobs the jobs screen hides.
type Activity interface {
	ListRecordings(ctx context.Context) ([]activity.Recording, error)
	JobStates(ctx context.Context, ids []shared.JobID) ([]activity.JobState, error)
}

// Assignments lists the assignments (cases/homeworkflow).
type Assignments interface {
	ListAssignments(ctx context.Context) ([]homeworkflow.AssignmentView, error)
}

// Inspector reads an upload with the quarantined reader (cases/library, TASK-122): a paid read whose answer is only
// suggestions (ADR-0022).
type Inspector interface {
	Inspect(ctx context.Context, id shared.MaterialID) (library.Inspected, error)
}

// Deliveries lists the email deliveries of an assignment (cases/senddelivery).
type Deliveries interface {
	ListForAssignment(ctx context.Context, a shared.AssignmentID) ([]senddelivery.View, error)
}

// tools runs the calls a step admits: the read tools, the paid readers inspect_upload (TASK-122) and answer_question
// (answer.go, TASK-118), the reversible actions (write.go, actions.go) and the proposals of class C (propose.go,
// TASK-127), which only make a confirm card. The turnkey tools come with later tasks and answer "unavailable" until
// then. A failing use case answers an error enum: no error text reaches the planner (ADR-0022).
type tools struct {
	library     SubjectLibrary
	inspector   Inspector // nil: inspect_upload answers "unavailable"
	answerer    Answerer  // nil: answer_question answers "unavailable"
	knowledge   Knowledge
	activity    Activity
	assignments Assignments
	deliveries  Deliveries
	journal     Journal
	actions     *Actions
	proposals   Proposals        // nil: propose_deadline and propose_send answer "unavailable"
	deletions   DeletionPreviews // nil: propose_delete_recording answers "unavailable"
	drafts      DraftVersions    // nil: list_assignments shows no draft version
	clock       Clock
	log         *slog.Logger
}

// read is one read tool: it returns the result for the planner and the IDs the result shows.
type read func(ctx context.Context, call values.Call, scope values.Scope) (values.ToolResult, []values.Ref, error)

func (t tools) reader(tool values.Tool) (read, bool) {
	switch tool {
	case values.ToolListSubjects:
		return t.listSubjects, true
	case values.ToolFindLessons:
		return t.findLessons, true
	case values.ToolListAssignments:
		return t.listAssignments, true
	case values.ToolJobStatus:
		return t.jobStatus, true
	case values.ToolDeliveryStatus:
		return t.deliveryStatus, true
	case values.ToolSearch:
		return t.search, true
	case values.ToolInspectUpload:
		return t.inspectUpload, t.inspector != nil
	case values.ToolAnswerQuestion:
		return t.answerQuestion, t.answerer != nil
	}
	return nil, false
}

// paidReads are the paid read tools this desktop runs: they are reads of the request, not its paid job (TASK-117).
var paidReads = []values.Tool{values.ToolInspectUpload, values.ToolAnswerQuestion}

// run runs one admitted call of step st and returns what it gave.
func (t tools) run(ctx context.Context, st agentturn.Step, c agentturn.Call) ran {
	switch c.Class {
	case actionpolicy.ClassU:
		return t.write(ctx, st, c)
	case actionpolicy.ClassC:
		return t.propose(ctx, st, c)
	}
	return ran{out: t.read(ctx, st.Scope, c)}
}

// read runs one admitted read call and returns its outcome.
func (t tools) read(ctx context.Context, scope values.Scope, c agentturn.Call) agentturn.Outcome {
	id := c.Call.ID()
	fn, ok := t.reader(c.Call.Tool())
	if !ok || c.Class != actionpolicy.ClassR || (c.Paid && !slices.Contains(paidReads, c.Call.Tool())) {
		return agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseUnavailable)}
	}
	res, seen, err := fn(ctx, c.Call, scope)
	switch {
	case errors.Is(err, knowledgebase.ErrInvalidQuery), errors.Is(err, answering.ErrInvalidQuestion):
		return agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseInvalidArgs)}
	case errors.Is(err, library.ErrMaterialNotFound), errors.Is(err, library.ErrMaterialRemoved):
		return agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseNotFound)}
	case err != nil:
		t.log.WarnContext(ctx, opTool, "tool", c.Call.Tool().String(), "err", err)
		return agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseFailed)}
	}
	return agentturn.Outcome{Result: res, Seen: seen}
}

// --- list_subjects -------------------------------------------------------------------------------------

func (t tools) listSubjects(ctx context.Context, call values.Call, _ values.Scope) (values.ToolResult, []values.Ref, error) {
	subjects, err := t.library.Subjects(ctx)
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	rows := make([]values.SubjectRow, 0, len(subjects))
	for _, s := range subjects {
		r, ok := refOfID(values.KindSubject, string(s.ID()))
		if !ok {
			continue
		}
		rows = append(rows, values.SubjectRow{ID: r, Name: s.Name().String(), Semester: s.Semester().Code()})
	}
	return values.SubjectsResult(call.ID(), rows)
}

// --- find_lessons --------------------------------------------------------------------------------------

func (t tools) findLessons(ctx context.Context, call values.Call, scope values.Scope) (values.ToolResult, []values.Ref, error) {
	q := values.LessonQueryOf(call.Args(), scope)
	lessons, err := t.lessonsOf(ctx, q.Subject)
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	// The schedule entry of a recording comes from the recordings screen; the knowledge base groups by subject.
	recs, err := t.activity.ListRecordings(ctx)
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	entry := make(map[shared.RecordingID]shared.LessonID, len(recs))
	for _, r := range recs {
		entry[r.ID] = r.LessonID
	}

	loc := t.clock.Now().Location()
	var rows []values.LessonRow
	for _, l := range kb.Newest(lessons) {
		rec, ok := refOfID(values.KindRecording, string(l.Recording))
		if !ok {
			continue
		}
		row := values.LessonRow{
			Recording: rec, Date: dateOf(l.StartedAt, loc), Kind: l.Kind, Transcribed: l.Ready.Transcribed(),
			Summary: l.Ready.Summary, LectureText: l.Ready.LectureText,
		}
		row.Lesson, _ = refOfID(values.KindLesson, string(entry[l.Recording]))
		row.Subject, _ = refOfID(values.KindSubject, string(l.Library.ID))
		if q.Match(row) {
			rows = append(rows, row)
		}
	}
	return values.LessonsResult(call.ID(), rows)
}

// lessonsOf returns the lessons of a library subject, or of every subject for a zero ref.
func (t tools) lessonsOf(ctx context.Context, sub values.Ref) ([]kb.Lesson, error) {
	if !sub.IsZero() {
		return t.knowledge.Lessons(ctx, kb.LibraryKey(shared.SubjectID(sub.ID())))
	}
	groups, err := t.knowledge.Subjects(ctx)
	if err != nil {
		return nil, err
	}
	var all []kb.Lesson
	for _, g := range groups {
		ls, err := t.knowledge.Lessons(ctx, g.Key)
		if err != nil {
			return nil, err
		}
		all = append(all, ls...)
	}
	return all, nil
}

// --- list_assignments ----------------------------------------------------------------------------------

func (t tools) listAssignments(ctx context.Context, call values.Call, scope values.Scope) (values.ToolResult, []values.Ref, error) {
	q := values.AssignmentQueryOf(call.Args(), scope)
	views, err := t.assignments.ListAssignments(ctx)
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	// The subject of an assignment is a free string; only the library subject it names is shown, never the string.
	var names []string
	for _, v := range views {
		if v.Subject != "" && !slices.Contains(names, v.Subject) {
			names = append(names, v.Subject)
		}
	}
	subjects, err := t.library.ResolveSubjects(ctx, names)
	if err != nil {
		return values.ToolResult{}, nil, err
	}

	now := t.clock.Now()
	today := dateOf(now, now.Location())
	var rows []values.AssignmentRow
	for _, v := range views {
		id, ok := refOfID(values.KindAssignment, string(v.ID))
		if !ok {
			continue
		}
		row := values.AssignmentRow{ID: id}
		if !v.Due.IsZero() {
			row.Due = dateOf(v.Due, now.Location())
		}
		row.Status = values.AssignmentStatusOf(row.Due, today)
		row.Lesson, _ = refOfID(values.KindLesson, string(v.LessonID))
		if m, ok := subjects[v.Subject]; ok && v.Subject != "" {
			row.Subject, _ = refOfID(values.KindSubject, string(m.ID))
		}
		if !q.Match(row) {
			continue
		}
		if row.Version, err = t.latestVersion(ctx, v.ID); err != nil {
			return values.ToolResult{}, nil, err
		}
		rows = append(rows, row)
	}
	return values.AssignmentsResult(call.ID(), rows)
}

// latestVersion is the latest draft version of an assignment, zero without a draft or without the drafts wired.
func (t tools) latestVersion(ctx context.Context, a shared.AssignmentID) (values.Ref, error) {
	if t.drafts == nil {
		return values.Ref{}, nil
	}
	versions, err := t.drafts.DraftVersions(ctx, a)
	if errors.Is(err, shared.ErrNotFound) {
		return values.Ref{}, nil
	}
	if err != nil {
		return values.Ref{}, err
	}
	for _, v := range versions {
		if v.Latest {
			r, err := values.VersionRef(v.Number)
			if err != nil {
				return values.Ref{}, nil
			}
			return r, nil
		}
	}
	return values.Ref{}, nil
}

// --- job_status ----------------------------------------------------------------------------------------

func (t tools) jobStatus(ctx context.Context, call values.Call, _ values.Scope) (values.ToolResult, []values.Ref, error) {
	ids := call.Args().IDs("ids")
	jobIDs := make([]shared.JobID, 0, len(ids))
	for _, id := range ids {
		jobIDs = append(jobIDs, shared.JobID(id.ID()))
	}
	jobs, err := t.activity.JobStates(ctx, jobIDs)
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	byID := make(map[shared.JobID]activity.JobState, len(jobs))
	for _, j := range jobs {
		byID[j.ID] = j
	}
	rows := make([]values.JobRow, 0, len(ids))
	for _, id := range ids {
		j, ok := byID[shared.JobID(id.ID())]
		if !ok {
			rows = append(rows, values.JobRow{ID: id, State: values.JobNotFound})
			continue
		}
		rows = append(rows, values.JobRow{ID: id, Work: workOf(j.Type), State: stateOf(j.Status), Percent: j.Percent})
	}
	return values.JobsResult(call.ID(), rows)
}

func workOf(t job.Type) values.JobWork {
	switch t {
	case transcribe.JobType:
		return values.WorkTranscription
	case homeworkflow.SummarizeJobType, homeworkflow.MaterialSummaryJobType:
		return values.WorkSummary
	case homeworkflow.ExtractHomeworkJobType, homeworkflow.MaterialHomeworkJobType:
		return values.WorkHomework
	case homeworkflow.LectureTextJobType:
		return values.WorkLectureText
	case homeworkflow.HomeworkDraftJobType:
		return values.WorkDraft
	case senddelivery.JobType:
		return values.WorkEmail
	case schedulesync.CalendarSyncJobType:
		return values.WorkCalendar
	}
	return values.WorkOther
}

func stateOf(s job.Status) values.JobState {
	switch s {
	case job.StatusQueued:
		return values.JobQueued
	case job.StatusRunning:
		return values.JobRunning
	case job.StatusRetryWait:
		return values.JobRetrying
	case job.StatusWaitingForNetwork:
		return values.JobWaitingNetwork
	case job.StatusWaitingForQuota:
		return values.JobWaitingQuota
	case job.StatusWaitingForPower:
		return values.JobWaitingPower
	case job.StatusCompleted:
		return values.JobDone
	case job.StatusCancelled:
		return values.JobCancelled
	}
	return values.JobFailed
}

// --- delivery_status -----------------------------------------------------------------------------------

func (t tools) deliveryStatus(ctx context.Context, call values.Call, _ values.Scope) (values.ToolResult, []values.Ref, error) {
	a, _ := call.Args().ID("assignment")
	views, err := t.deliveries.ListForAssignment(ctx, shared.AssignmentID(a.ID()))
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	rows := make([]values.DeliveryRow, 0, len(views))
	for _, v := range views {
		rows = append(rows, values.DeliveryRow{State: deliveryStateOf(v.Status), Attempts: v.Attempts})
	}
	return values.DeliveriesResult(call.ID(), a, rows)
}

func deliveryStateOf(s delivery.Status) values.DeliveryState {
	switch s {
	case delivery.StatusPending:
		return values.DeliveryPending
	case delivery.StatusSending:
		return values.DeliverySending
	case delivery.StatusSent:
		return values.DeliverySent
	case delivery.StatusFailed:
		return values.DeliveryFailed
	}
	return values.DeliveryUnknown
}

// --- search --------------------------------------------------------------------------------------------

func (t tools) search(ctx context.Context, call values.Call, scope values.Scope) (values.ToolResult, []values.Ref, error) {
	query, _ := call.Args().Query("query")
	sub := values.SearchSubjectOf(call.Args(), scope)
	found, err := t.knowledge.Search(ctx, string(query))
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	c := values.SearchCounts{Sections: map[values.SearchSection]int{}, Truncated: found.Truncated}
	for _, h := range found.Hits {
		if !sub.IsZero() && string(h.Lesson.Library.ID) != sub.ID() {
			continue
		}
		rec, ok := refOfID(values.KindRecording, string(h.Recording))
		if !ok {
			continue
		}
		section, ok := sectionOf(h.Section)
		if !ok {
			continue
		}
		c.Matches++
		c.Sections[section]++
		c.Recordings = append(c.Recordings, rec)
	}
	return values.SearchResult(call.ID(), c)
}

func sectionOf(s kb.Section) (values.SearchSection, bool) {
	switch s {
	case kb.SectionTitle:
		return values.SectionTitle, true
	case kb.SectionSummary:
		return values.SectionSummary, true
	case kb.SectionLectureText:
		return values.SectionLectureText, true
	case kb.SectionTranscript:
		return values.SectionTranscript, true
	}
	return "", false
}

// --- inspect_upload ------------------------------------------------------------------------------------

// inspectUpload reads an upload with the quarantined reader. The planner gets the upload as a material (the same
// file, for make_summary and find_homework), how far its text was read and the reader's enum, ID, date and flags;
// never the text, the file name or the reader's output (ADR-0022).
func (t tools) inspectUpload(ctx context.Context, call values.Call, _ values.Scope) (values.ToolResult, []values.Ref, error) {
	up, _ := call.Args().ID("upload")
	got, err := t.inspector.Inspect(ctx, shared.MaterialID(up.ID()))
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	row := values.InspectRow{Upload: up, Text: textStateOf(got)}
	row.Material, _ = refOfID(values.KindMaterial, up.ID())
	if a := got.Answer; a != nil {
		row.DocKind = string(a.DocKind)
		row.Subject, _ = refOfID(values.KindSubject, string(a.Subject))
		if !a.LessonDate.IsZero() {
			row.LessonDate, _ = values.NewDate(a.LessonDate.Year(), a.LessonDate.Month(), a.LessonDate.Day())
		}
		row.HasHomework, row.HasTicketList, row.MentionsEmail = a.HasHomework, a.HasTicketList, a.MentionsEmail
	}
	return values.InspectResult(call.ID(), row)
}

func textStateOf(in library.Inspected) values.TextState {
	switch {
	case in.Text == material.TextPDFNotExtracted:
		return values.TextNotRead
	case in.Text == material.TextFailed:
		return values.TextUnreadable
	case in.Empty:
		return values.TextEmpty
	}
	return values.TextRead
}

// --- helpers -------------------------------------------------------------------------------------------

// refOfID makes a typed ref of an ID another context gave; an empty ID or one the agent cannot address is none.
func refOfID(kind values.IDKind, id string) (values.Ref, bool) {
	if id == "" {
		return values.Ref{}, false
	}
	r, err := values.NewRef(kind, id)
	if err != nil {
		return values.Ref{}, false
	}
	return r, true
}

// dateOf is the calendar day of t in loc; zero outside the years a Date holds.
func dateOf(t time.Time, loc *time.Location) values.Date {
	t = t.In(loc)
	d, err := values.NewDate(t.Year(), t.Month(), t.Day())
	if err != nil {
		return values.Date{}
	}
	return d
}
