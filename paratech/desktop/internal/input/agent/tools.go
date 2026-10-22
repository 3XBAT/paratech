package agent

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
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

// Deliveries lists the email deliveries of an assignment (cases/senddelivery).
type Deliveries interface {
	ListForAssignment(ctx context.Context, a shared.AssignmentID) ([]senddelivery.View, error)
}

// tools runs the calls a step admits. Only the read tools of catalog v1 run here; the paid readers
// (answer_question, inspect_upload), the reversible actions and the proposals come with later tasks and answer
// "unavailable" until then. A failing use case answers "failed": no error text reaches the planner (ADR-0022).
type tools struct {
	library     SubjectLibrary
	knowledge   Knowledge
	activity    Activity
	assignments Assignments
	deliveries  Deliveries
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
	}
	return nil, false
}

// run runs one admitted call and returns its outcome.
func (t tools) run(ctx context.Context, scope values.Scope, c agentturn.Call) agentturn.Outcome {
	id := c.Call.ID()
	fn, ok := t.reader(c.Call.Tool())
	if !ok || c.Class != actionpolicy.ClassR || c.Paid {
		return agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseUnavailable)}
	}
	res, seen, err := fn(ctx, c.Call, scope)
	switch {
	case errors.Is(err, knowledgebase.ErrInvalidQuery):
		return agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseInvalidArgs)}
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
		if q.Match(row) {
			rows = append(rows, row)
		}
	}
	return values.AssignmentsResult(call.ID(), rows)
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
	case homeworkflow.SummarizeJobType:
		return values.WorkSummary
	case homeworkflow.ExtractHomeworkJobType:
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
	query, _ := call.Args().Label("query")
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
