package values

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
)

// Results of the read tools (ADR-0022): built here from agent-owned rows of IDs, dates, enums and counters, which the
// Driver fills from the views of other contexts. No row has a free-text field except a subject name checked as a
// Name, so titles, transcripts and model output never reach the planner.

// MaxReadItems bounds the rows of one read result; more are cut and the result says truncated.
const MaxReadItems = 50

// AssignmentStatus is the state of an assignment's deadline on a day.
type AssignmentStatus string

// Assignment statuses. The tool filter also accepts "any".
const (
	AssignmentUpcoming AssignmentStatus = "upcoming"
	AssignmentOverdue  AssignmentStatus = "overdue"
	AssignmentNoDue    AssignmentStatus = "no_due"
)

// AssignmentStatusOf says whether a deadline is upcoming (today or later) or overdue on day today.
func AssignmentStatusOf(due, today Date) AssignmentStatus {
	switch {
	case due.IsZero():
		return AssignmentNoDue
	case due.Before(today):
		return AssignmentOverdue
	}
	return AssignmentUpcoming
}

// JobWork is what a job does, as the planner sees it.
type JobWork string

// Job works.
const (
	WorkTranscription JobWork = "transcription"
	WorkSummary       JobWork = "summary"
	WorkHomework      JobWork = "homework"
	WorkLectureText   JobWork = "lecture_text"
	WorkDraft         JobWork = "draft"
	WorkEmail         JobWork = "email"
	WorkCalendar      JobWork = "calendar"
	WorkOther         JobWork = "other"
)

// JobState is the state of a job, as the planner sees it.
type JobState string

// Job states. JobNotFound answers an ID with no job.
const (
	JobQueued         JobState = "queued"
	JobRunning        JobState = "running"
	JobRetrying       JobState = "retrying"
	JobWaitingNetwork JobState = "waiting_network"
	JobWaitingQuota   JobState = "waiting_quota"
	JobWaitingPower   JobState = "waiting_power"
	JobDone           JobState = "done"
	JobFailed         JobState = "failed"
	JobCancelled      JobState = "cancelled"
	JobNotFound       JobState = "not_found"
)

// DeliveryState is the state of an email delivery, as the planner sees it.
type DeliveryState string

// Delivery states.
const (
	DeliveryPending DeliveryState = "pending"
	DeliverySending DeliveryState = "sending"
	DeliverySent    DeliveryState = "sent"
	DeliveryFailed  DeliveryState = "failed"
	DeliveryUnknown DeliveryState = "unknown"
)

// SearchSection is a part of a lesson a search match was found in.
type SearchSection string

// Search sections.
const (
	SectionTitle       SearchSection = "title"
	SectionSummary     SearchSection = "summary"
	SectionLectureText SearchSection = "lecture_text"
	SectionTranscript  SearchSection = "transcript"
)

// SubjectRow is one subject of list_subjects. Name is shown only if it is a valid Name (ADR-0022), else left out.
type SubjectRow struct {
	ID       Ref
	Name     string
	Semester string // code "<year>-autumn|spring"; anything else is left out
}

// LessonRow is one recorded lesson of find_lessons.
type LessonRow struct {
	Recording   Ref
	Lesson      Ref // the schedule entry; zero for an ad-hoc recording
	Subject     Ref // zero when the lesson names no subject of the library
	Date        Date
	Kind        string // a lesson kind code; anything else is left out
	Transcribed bool
	Summary     bool
	LectureText bool
}

// AssignmentRow is one assignment of list_assignments, without its title (a model wrote it).
type AssignmentRow struct {
	ID      Ref
	Subject Ref
	Lesson  Ref
	Due     Date
	Status  AssignmentStatus
}

// JobRow is one job of job_status.
type JobRow struct {
	ID      Ref
	Work    JobWork
	State   JobState
	Percent int
}

// DeliveryRow is one delivery of delivery_status.
type DeliveryRow struct {
	State    DeliveryState
	Attempts int
}

// SearchCounts is the result of search: counters and the recordings with a match, no snippets.
type SearchCounts struct {
	Matches    int
	Sections   map[SearchSection]int
	Recordings []Ref
	Truncated  bool
}

// --- queries -----------------------------------------------------------------------------------------

// subjectOf is the subject argument, else the subject of the scope (zero: every subject).
func subjectOf(args Args, scope Scope) Ref {
	if r, ok := args.ID("subject"); ok {
		return r
	}
	return scope.Subject()
}

// LessonQuery is the filter of find_lessons; zero fields match everything.
type LessonQuery struct {
	Subject Ref
	From    Date
	To      Date
	Kind    string
}

// LessonQueryOf reads the arguments of find_lessons; without a subject it is the subject of the scope.
func LessonQueryOf(args Args, scope Scope) LessonQuery {
	q := LessonQuery{Subject: subjectOf(args, scope)}
	q.From, _ = args.Date("from")
	q.To, _ = args.Date("to")
	q.Kind, _ = args.Enum("kind")
	return q
}

// Match reports whether the row passes the filter; dates are inclusive.
func (q LessonQuery) Match(r LessonRow) bool {
	switch {
	case !q.Subject.IsZero() && r.Subject != q.Subject:
		return false
	case q.Kind != "" && r.Kind != q.Kind:
		return false
	case !q.From.IsZero() && r.Date.Before(q.From):
		return false
	case !q.To.IsZero() && q.To.Before(r.Date):
		return false
	}
	return true
}

// AssignmentQuery is the filter of list_assignments; zero fields match everything.
type AssignmentQuery struct {
	Subject   Ref
	DueBefore Date
	Status    AssignmentStatus // "" for any
}

// AssignmentQueryOf reads the arguments of list_assignments; without a subject it is the subject of the scope.
func AssignmentQueryOf(args Args, scope Scope) AssignmentQuery {
	q := AssignmentQuery{Subject: subjectOf(args, scope)}
	q.DueBefore, _ = args.Date("due_before")
	if s, ok := args.Enum("status"); ok && s != "any" {
		q.Status = AssignmentStatus(s)
	}
	return q
}

// Match reports whether the row passes the filter; due_before is inclusive and drops rows without a deadline.
func (q AssignmentQuery) Match(r AssignmentRow) bool {
	switch {
	case !q.Subject.IsZero() && r.Subject != q.Subject:
		return false
	case q.Status != "" && r.Status != q.Status:
		return false
	case !q.DueBefore.IsZero() && (r.Due.IsZero() || q.DueBefore.Before(r.Due)):
		return false
	}
	return true
}

// SearchSubjectOf is the subject a search is limited to: the subject of the scope unless scope is "all" (zero: every
// subject).
func SearchSubjectOf(args Args, scope Scope) Ref {
	if s, ok := args.Enum("scope"); ok && s == "all" {
		return Ref{}
	}
	return scope.Subject()
}

// --- results -----------------------------------------------------------------------------------------

// SubjectsResult is the result of list_subjects and the IDs it shows.
func SubjectsResult(callID string, rows []SubjectRow) (ToolResult, []Ref, error) {
	type item struct {
		ID       string `json:"id"`
		Name     string `json:"name,omitempty"`
		Semester string `json:"semester,omitempty"`
	}
	items := make([]item, 0, len(rows))
	refs := make([]Ref, 0, len(rows))
	for _, r := range rows {
		if err := wantRef(r.ID, KindSubject, true); err != nil {
			return ToolResult{}, nil, err
		}
		it := item{ID: r.ID.ID()}
		if n, err := ParseName(r.Name); err == nil {
			it.Name = string(n)
		}
		if validSemester(r.Semester) {
			it.Semester = r.Semester
		}
		items = append(items, it)
		refs = append(refs, r.ID)
	}
	return listResult(callID, "subjects", items, refs)
}

// LessonsResult is the result of find_lessons and the IDs it shows.
func LessonsResult(callID string, rows []LessonRow) (ToolResult, []Ref, error) {
	type item struct {
		Recording   string `json:"recording"`
		Lesson      string `json:"lesson,omitempty"`
		Subject     string `json:"subject,omitempty"`
		Date        string `json:"date,omitempty"`
		Kind        string `json:"kind,omitempty"`
		Transcribed bool   `json:"transcribed"`
		Summary     bool   `json:"summary"`
		LectureText bool   `json:"lecture_text"`
	}
	items := make([]item, 0, len(rows))
	refs := make([]Ref, 0, len(rows))
	for _, r := range rows {
		if err := errorsOf(wantRef(r.Recording, KindRecording, true), wantRef(r.Lesson, KindLesson, false),
			wantRef(r.Subject, KindSubject, false)); err != nil {
			return ToolResult{}, nil, err
		}
		it := item{
			Recording: r.Recording.ID(), Lesson: r.Lesson.ID(), Subject: r.Subject.ID(), Date: r.Date.String(),
			Transcribed: r.Transcribed, Summary: r.Summary, LectureText: r.LectureText,
		}
		if slices.Contains(lessonKinds, r.Kind) {
			it.Kind = r.Kind
		}
		items = append(items, it)
		refs = append(refs, r.Recording, r.Lesson, r.Subject)
	}
	return listResult(callID, "lessons", items, refs)
}

// AssignmentsResult is the result of list_assignments and the IDs it shows.
func AssignmentsResult(callID string, rows []AssignmentRow) (ToolResult, []Ref, error) {
	type item struct {
		ID      string `json:"id"`
		Subject string `json:"subject,omitempty"`
		Lesson  string `json:"lesson,omitempty"`
		Due     string `json:"due,omitempty"`
		Status  string `json:"status"`
	}
	items := make([]item, 0, len(rows))
	refs := make([]Ref, 0, len(rows))
	for _, r := range rows {
		if err := errorsOf(wantRef(r.ID, KindAssignment, true), wantRef(r.Subject, KindSubject, false),
			wantRef(r.Lesson, KindLesson, false)); err != nil {
			return ToolResult{}, nil, err
		}
		if !slices.Contains([]AssignmentStatus{AssignmentUpcoming, AssignmentOverdue, AssignmentNoDue}, r.Status) {
			return ToolResult{}, nil, fmt.Errorf("%w: unknown assignment status", ErrInvalidResult)
		}
		items = append(items, item{
			ID: r.ID.ID(), Subject: r.Subject.ID(), Lesson: r.Lesson.ID(), Due: r.Due.String(), Status: string(r.Status),
		})
		refs = append(refs, r.ID, r.Subject, r.Lesson)
	}
	return listResult(callID, "assignments", items, refs)
}

var (
	jobWorks = []JobWork{
		WorkTranscription, WorkSummary, WorkHomework, WorkLectureText, WorkDraft, WorkEmail, WorkCalendar, WorkOther,
	}
	jobStates = []JobState{
		JobQueued, JobRunning, JobRetrying, JobWaitingNetwork, JobWaitingQuota, JobWaitingPower, JobDone, JobFailed,
		JobCancelled, JobNotFound,
	}
	deliveryStates = []DeliveryState{DeliveryPending, DeliverySending, DeliverySent, DeliveryFailed, DeliveryUnknown}
)

// JobsResult is the result of job_status and the IDs it shows. A job that is not found has only its state.
func JobsResult(callID string, rows []JobRow) (ToolResult, []Ref, error) {
	type item struct {
		ID      string `json:"id"`
		Work    string `json:"work,omitempty"`
		Status  string `json:"status"`
		Percent int    `json:"percent"`
	}
	items := make([]item, 0, len(rows))
	refs := make([]Ref, 0, len(rows))
	for _, r := range rows {
		if err := wantRef(r.ID, KindJob, true); err != nil {
			return ToolResult{}, nil, err
		}
		if !slices.Contains(jobStates, r.State) || (r.Work != "" && !slices.Contains(jobWorks, r.Work)) {
			return ToolResult{}, nil, fmt.Errorf("%w: unknown job work or state", ErrInvalidResult)
		}
		items = append(items, item{ID: r.ID.ID(), Work: string(r.Work), Status: string(r.State), Percent: min(max(r.Percent, 0), 100)})
		refs = append(refs, r.ID)
	}
	return listResult(callID, "jobs", items, refs)
}

// DeliveriesResult is the result of delivery_status of one assignment, newest delivery first.
func DeliveriesResult(callID string, assignment Ref, rows []DeliveryRow) (ToolResult, []Ref, error) {
	if err := wantRef(assignment, KindAssignment, true); err != nil {
		return ToolResult{}, nil, err
	}
	type item struct {
		Status   string `json:"status"`
		Attempts int    `json:"attempts"`
	}
	items := make([]item, 0, len(rows))
	for _, r := range rows {
		if !slices.Contains(deliveryStates, r.State) {
			return ToolResult{}, nil, fmt.Errorf("%w: unknown delivery state", ErrInvalidResult)
		}
		items = append(items, item{Status: string(r.State), Attempts: max(r.Attempts, 0)})
	}
	if len(items) > MaxReadItems {
		items = items[:MaxReadItems]
	}
	body := map[string]any{"assignment": assignment.ID(), "deliveries": items, "count": len(rows)}
	return ToolResult{CallID: callID, ResultJSON: mustJSON(body)}, []Ref{assignment}, nil
}

// SearchResult is the result of search and the IDs it shows: counters and recording IDs only.
func SearchResult(callID string, c SearchCounts) (ToolResult, []Ref, error) {
	sections := map[string]int{}
	for s, n := range c.Sections {
		if !slices.Contains([]SearchSection{SectionTitle, SectionSummary, SectionLectureText, SectionTranscript}, s) {
			return ToolResult{}, nil, fmt.Errorf("%w: unknown search section", ErrInvalidResult)
		}
		sections[string(s)] = max(n, 0)
	}
	recs := RefSet{}
	ids := make([]string, 0, min(len(c.Recordings), MaxReadItems))
	refs := make([]Ref, 0, cap(ids))
	cut := false
	for _, r := range c.Recordings {
		if err := wantRef(r, KindRecording, true); err != nil {
			return ToolResult{}, nil, err
		}
		if recs.Has(r) {
			continue
		}
		recs.Add(r)
		if len(ids) == MaxReadItems {
			cut = true
			continue
		}
		ids = append(ids, r.ID())
		refs = append(refs, r)
	}
	body := map[string]any{
		"matches": max(c.Matches, 0), "lessons": len(recs), "recordings": ids, "sections": sections,
		"truncated": c.Truncated || cut,
	}
	return ToolResult{CallID: callID, ResultJSON: mustJSON(body)}, refs, nil
}

// listResult encodes {"<name>": items, "count": n, "truncated": bool} with at most MaxReadItems items and within
// MaxResultBytes; refs are the IDs of the items shown, zero refs dropped.
func listResult[T any](callID, name string, items []T, refs []Ref) (ToolResult, []Ref, error) {
	total := len(items)
	n := min(total, MaxReadItems)
	for {
		body := map[string]any{name: items[:n], "count": total, "truncated": n < total}
		b, err := json.Marshal(body)
		if err != nil {
			return ToolResult{}, nil, fmt.Errorf("%w: %w", ErrInvalidResult, err)
		}
		if len(b) <= MaxResultBytes || n == 0 {
			shown := RefSet{}
			if total > 0 {
				per := len(refs) / total // every row adds the same number of refs
				shown.Add(refs[:n*per]...)
			}
			return ToolResult{CallID: callID, ResultJSON: string(b)}, shown.Sorted(), nil
		}
		n /= 2
	}
}

func wantRef(r Ref, kind IDKind, required bool) error {
	if r.IsZero() {
		if required {
			return fmt.Errorf("%w: a %s id is required", ErrInvalidResult, kind)
		}
		return nil
	}
	if r.Kind() != kind {
		return fmt.Errorf("%w: want a %s id, got %s", ErrInvalidResult, kind, r.Kind())
	}
	return nil
}

func errorsOf(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// validSemester accepts the semester code "<year>-autumn" or "<year>-spring" of years 2000..2100.
func validSemester(code string) bool {
	if len(code) < 6 || code[4] != '-' {
		return false
	}
	year, err := strconv.Atoi(code[:4])
	if err != nil || year < 2000 || year > 2100 {
		return false
	}
	half := code[5:]
	return half == "autumn" || half == "spring"
}
