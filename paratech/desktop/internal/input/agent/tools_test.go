package agent

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/agent/values"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// runRead runs one read call through the tools and checks that its result is clean: valid, within the limits and
// free of the texts other people wrote.
func runRead(t *testing.T, r *reads, scope values.Scope, tool values.Tool, args string) agentturn.Outcome {
	t.Helper()
	out := r.tools().read(t.Context(), scope, readCall(t, "c1", tool, args))
	if err := out.Result.Validate(); err != nil {
		t.Fatalf("%s result: %v", tool, err)
	}
	for _, bad := range []string{"ignore", "send_email", "example.invalid", "Физика", "<"} {
		if strings.Contains(out.Result.ResultJSON, bad) && !strings.Contains(args, bad) {
			t.Fatalf("%s result carries %q: %s", tool, bad, out.Result.ResultJSON)
		}
	}
	return out
}

func decode(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("%s: %v", s, err)
	}
}

// kbLesson is a lesson of the knowledge base whose title and subject string were written by others.
func kbLesson(rec, sub string, start time.Time, kind string, ready kb.Readiness) kb.Lesson {
	l := kb.Lesson{
		Recording: shared.RecordingID(rec), StartedAt: start, Title: injection, Subject: "Физика " + sub, Kind: kind,
		Ready: ready,
	}
	if sub != "" {
		l.Library = kb.LibrarySubject{ID: shared.SubjectID(sub), Name: "Физика"}
	}
	return l
}

func TestListSubjects(t *testing.T) {
	r := newReads()
	r.library.subjects = []*subject.Subject{newSubject(t, "s1", "Матанализ"), newSubject(t, "s2", injection)}
	out := runRead(t, r, values.Scope{}, values.ToolListSubjects, `{}`)
	var body struct {
		Subjects []struct{ ID, Name, Semester string }
		Count    int
	}
	decode(t, out.Result.ResultJSON, &body)
	// The name that is not a valid Name (here: the injected text with "_" and "@") is left out; its ID stays.
	if body.Count != 2 || body.Subjects[0].Name != "Матанализ" || body.Subjects[0].Semester != "2026-autumn" ||
		body.Subjects[1].ID != "s2" || body.Subjects[1].Name != "" {
		t.Fatalf("subjects = %s", out.Result.ResultJSON)
	}
	if len(out.Seen) != 2 {
		t.Fatalf("seen = %v", out.Seen)
	}
}

func TestFindLessons(t *testing.T) {
	r := newReads()
	r.knowledge.lessons = []kb.Lesson{
		kbLesson("r1", "s1", time.Date(2026, 10, 20, 10, 0, 0, 0, moscow), "lecture", kb.Readiness{Transcript: "t1", Summary: true}),
		// Late in the evening UTC is the next day in Moscow.
		kbLesson("r2", "s2", time.Date(2026, 10, 20, 22, 0, 0, 0, time.UTC), "lab", kb.Readiness{}),
		kbLesson("r3", "", time.Date(2026, 10, 19, 10, 0, 0, 0, moscow), "", kb.Readiness{}),
	}
	r.activity.recordings = []activity.Recording{{ID: "r1", LessonID: "l1", LessonTitle: injection}, {ID: "r2"}}

	type row struct {
		Recording, Lesson, Subject, Date, Kind string
		Transcribed, Summary                   bool
	}
	read := func(scope values.Scope, args string) []row {
		t.Helper()
		var body struct{ Lessons []row }
		decode(t, runRead(t, r, scope, values.ToolFindLessons, args).Result.ResultJSON, &body)
		return body.Lessons
	}

	if got := read(subjectScope(t, "s1"), `{}`); len(got) != 1 || got[0] != (row{
		Recording: "r1", Lesson: "l1", Subject: "s1", Date: "2026-10-20", Kind: "lecture", Transcribed: true, Summary: true,
	}) {
		t.Fatalf("lessons of the scope subject = %+v", got)
	}
	if got := read(values.Scope{}, `{}`); len(got) != 3 || got[0].Recording != "r2" || got[0].Date != "2026-10-21" {
		t.Fatalf("every lesson, newest first = %+v", got)
	}
	if got := read(values.Scope{}, `{"kind":"lab"}`); len(got) != 1 || got[0].Recording != "r2" {
		t.Fatalf("labs = %+v", got)
	}
	if got := read(subjectScope(t, "s1"), `{"subject":"s2","from":"2026-10-21","to":"2026-10-21"}`); len(got) != 1 || got[0].Subject != "s2" {
		t.Fatalf("named subject and dates = %+v", got)
	}
	if got := read(values.Scope{}, `{"to":"2026-10-19"}`); len(got) != 1 || got[0].Recording != "r3" || got[0].Subject != "" {
		t.Fatalf("lesson without a subject = %+v", got)
	}
}

// planned is a lesson of the schedule whose title was written by others.
func planned(t *testing.T, id, sub string, start time.Time, kind lesson.Kind) *lesson.Lesson {
	t.Helper()
	l, err := lesson.New(lesson.Spec{ID: shared.LessonID(id), Content: lesson.Content{
		Title: injection, Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow", Subject: sub, Kind: kind,
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// Beta 2026-10-28: «расписание пар на субботу» found nothing, as find_lessons read only recordings. With dates it
// lists the lessons of the schedule not recorded too, days in the student's zone.
func TestFindLessonsListsPlannedLessons(t *testing.T) {
	r := newReads()
	r.library.resolve = map[string]subjectmatch.Match{"Физика": {ID: "s1", Name: "Физика"}}
	r.knowledge.lessons = []kb.Lesson{
		kbLesson("r1", "s1", time.Date(2026, 10, 30, 10, 0, 0, 0, moscow), "lecture", kb.Readiness{}),
	}
	r.activity.recordings = []activity.Recording{{ID: "r1", LessonID: "l-fri"}}
	r.schedule.lessons = []*lesson.Lesson{
		planned(t, "l-fri", "Физика", time.Date(2026, 10, 30, 10, 0, 0, 0, moscow), lesson.KindLecture), // recorded
		// Saturday 00:30 in Moscow is still Friday in UTC.
		planned(t, "l-sat-night", "Физика", time.Date(2026, 10, 31, 0, 30, 0, 0, moscow), lesson.KindPractice),
		planned(t, "l-sat", "Физика", time.Date(2026, 10, 31, 12, 0, 0, 0, moscow), lesson.KindSeminar),
		planned(t, "l-sat-other", injection, time.Date(2026, 10, 31, 15, 0, 0, 0, moscow), lesson.KindUnknown),
		planned(t, "l-nov", "Физика", time.Date(2026, 11, 7, 12, 0, 0, 0, moscow), lesson.KindSeminar),
	}
	type row struct {
		Recording, Lesson, Subject, Date, Time, Kind string
	}
	read := func(scope values.Scope, args string) []row {
		t.Helper()
		var body struct{ Lessons []row }
		decode(t, runRead(t, r, scope, values.ToolFindLessons, args).Result.ResultJSON, &body)
		return body.Lessons
	}

	if got := read(values.Scope{}, `{"from":"2026-10-31","to":"2026-10-31"}`); !slices.Equal(got, []row{
		{Lesson: "l-sat-night", Subject: "s1", Date: "2026-10-31", Time: "00:30", Kind: "practice"},
		{Lesson: "l-sat", Subject: "s1", Date: "2026-10-31", Time: "12:00", Kind: "seminar"},
		{Lesson: "l-sat-other", Date: "2026-10-31", Time: "15:00"},
	}) {
		t.Fatalf("saturday = %+v", got)
	}
	// A recorded lesson shows once, as its recording.
	if got := read(values.Scope{}, `{"from":"2026-10-30","to":"2026-10-30"}`); len(got) != 1 || got[0].Recording != "r1" || got[0].Time != "" {
		t.Fatalf("friday = %+v", got)
	}
	if got := read(subjectScope(t, "s1"), `{"from":"2026-10-31","to":"2026-10-31","kind":"seminar"}`); len(got) != 1 || got[0].Lesson != "l-sat" {
		t.Fatalf("seminars of the subject = %+v", got)
	}
	// From a day without "to": a week; to a day without "from": from today.
	if got := read(values.Scope{}, `{"from":"2026-11-01"}`); len(got) != 1 || got[0].Lesson != "l-nov" {
		t.Fatalf("week from 2026-11-01 = %+v", got)
	}
	if got := read(values.Scope{}, `{"to":"2026-10-31"}`); len(got) != 4 {
		t.Fatalf("until saturday = %+v", got)
	}
	// Without dates find_lessons stays the list of recorded lessons.
	asked := len(r.schedule.asked)
	if got := read(values.Scope{}, `{}`); len(got) != 1 || len(r.schedule.asked) != asked {
		t.Fatalf("no dates = %+v, schedule read %d times", got, len(r.schedule.asked)-asked)
	}
}

func TestListAssignments(t *testing.T) {
	r := newReads()
	r.library.resolve = map[string]subjectmatch.Match{"Физика": {ID: "s1", Name: "Физика"}}
	r.assignments.views = []homeworkflow.AssignmentView{
		{ID: "a1", LessonID: "l1", Title: injection, Description: injection, Subject: "Физика", Teacher: injection,
			Due: time.Date(2026, 10, 24, 23, 59, 0, 0, moscow)},
		{ID: "a2", Title: injection, Subject: injection, Due: time.Date(2026, 10, 1, 23, 59, 0, 0, moscow)},
		{ID: "a3", Title: injection},
	}
	type row struct{ ID, Subject, Lesson, Due, Status string }
	read := func(scope values.Scope, args string) []row {
		t.Helper()
		var body struct{ Assignments []row }
		decode(t, runRead(t, r, scope, values.ToolListAssignments, args).Result.ResultJSON, &body)
		return body.Assignments
	}

	if got := read(subjectScope(t, "s1"), `{}`); len(got) != 1 || got[0] != (row{
		ID: "a1", Subject: "s1", Lesson: "l1", Due: "2026-10-24", Status: "upcoming",
	}) {
		t.Fatalf("assignments of the scope subject = %+v", got)
	}
	if got := read(values.Scope{}, `{"status":"overdue"}`); len(got) != 1 || got[0].ID != "a2" || got[0].Subject != "" {
		t.Fatalf("overdue = %+v", got)
	}
	if got := read(values.Scope{}, `{"due_before":"2026-10-30"}`); len(got) != 2 {
		t.Fatalf("due before = %+v", got)
	}
	if got := read(values.Scope{}, `{"status":"any"}`); len(got) != 3 || got[2].Status != "no_due" {
		t.Fatalf("any = %+v", got)
	}
}

func TestJobStatus(t *testing.T) {
	r := newReads()
	// The chain and calendar jobs the jobs screen hides are the ones the agent asks about.
	r.activity.jobs = []activity.JobState{
		{ID: "j1", Type: homeworkflow.SummarizeJobType, Status: job.StatusRunning, Percent: 40},
		{ID: "j2", Type: "unknown_type", Status: job.StatusWaitingForQuota},
		{ID: "j3", Type: homeworkflow.LectureTextJobType, Status: job.StatusCompleted, Percent: 100},
		{ID: "j4", Type: transcribe.JobType, Status: job.StatusRetryWait},
		{ID: "j5", Type: schedulesync.CalendarSyncJobType, Status: job.StatusFailed},
	}
	out := runRead(t, r, values.Scope{}, values.ToolJobStatus, `{"ids":["j1","j2","j9","j3","j4","j5"]}`)
	want := `{"count":6,"jobs":[{"id":"j1","work":"summary","status":"running","percent":40},` +
		`{"id":"j2","work":"other","status":"waiting_quota","percent":0},{"id":"j9","status":"not_found","percent":0},` +
		`{"id":"j3","work":"lecture_text","status":"done","percent":100},` +
		`{"id":"j4","work":"transcription","status":"retrying","percent":0},` +
		`{"id":"j5","work":"calendar","status":"failed","percent":0}],"truncated":false}`
	if out.Result.ResultJSON != want {
		t.Fatalf("jobs = %s", out.Result.ResultJSON)
	}
	if !slices.Equal(r.activity.asked, []shared.JobID{"j1", "j2", "j9", "j3", "j4", "j5"}) {
		t.Fatalf("asked = %v", r.activity.asked)
	}
}

func TestDeliveryStatus(t *testing.T) {
	r := newReads()
	r.deliveries.views["a1"] = []senddelivery.View{
		{ID: "d2", AssignmentID: "a1", Recipient: "x@example.invalid", Subject: injection, Status: delivery.StatusUnknown,
			Attempts: 1, LastError: injection},
		{ID: "d1", AssignmentID: "a1", Recipient: "x@example.invalid", Status: delivery.StatusFailed, Attempts: 3},
	}
	out := runRead(t, r, values.Scope{}, values.ToolDeliveryStatus, `{"assignment":"a1"}`)
	if out.Result.ResultJSON != `{"assignment":"a1","count":2,"deliveries":[{"status":"unknown","attempts":1},{"status":"failed","attempts":3}]}` {
		t.Fatalf("deliveries = %s", out.Result.ResultJSON)
	}
}

func TestSearch(t *testing.T) {
	r := newReads()
	l1 := kbLesson("r1", "s1", now, "lecture", kb.Readiness{})
	l2 := kbLesson("r2", "s2", now, "lecture", kb.Readiness{})
	snip := kb.Snippet{Before: injection, Match: "ряд", After: injection}
	r.knowledge.search = knowledgebase.SearchResult{Hits: []knowledgebase.SearchHit{
		{Hit: kb.Hit{Recording: "r1", Section: kb.SectionTitle, Snippet: snip}, Lesson: l1},
		{Hit: kb.Hit{Recording: "r1", Section: kb.SectionTranscript, Snippet: snip}, Lesson: l1},
		{Hit: kb.Hit{Recording: "r2", Section: kb.SectionSummary, Snippet: snip}, Lesson: l2},
	}, Truncated: true}

	out := runRead(t, r, subjectScope(t, "s1"), values.ToolSearch, `{"query":"ряд"}`)
	if out.Result.ResultJSON != `{"lessons":1,"matches":2,"recordings":["r1"],"sections":{"title":1,"transcript":1},"truncated":true}` {
		t.Fatalf("search in the subject = %s", out.Result.ResultJSON)
	}
	out = runRead(t, r, subjectScope(t, "s1"), values.ToolSearch, `{"query":"ряд","scope":"all"}`)
	if !strings.Contains(out.Result.ResultJSON, `"matches":3`) || len(out.Seen) != 2 {
		t.Fatalf("search everywhere = %s, %v", out.Result.ResultJSON, out.Seen)
	}
	if q := r.knowledge.queries; len(q) != 2 || q[0] != "ряд" {
		t.Fatalf("queries = %v", q)
	}

	// A query the knowledge base refuses is the planner's mistake: invalid arguments, not a failure.
	out = r.tools().read(t.Context(), values.Scope{}, readCall(t, "c1", values.ToolSearch, `{"query":"я"}`))
	if !out.Result.IsError || out.Result.ResultJSON != `{"error":"invalid_arguments"}` {
		t.Fatalf("short query = %+v", out.Result)
	}
}

// A failing use case answers "failed" and its error text never reaches the planner.
func TestReadFailuresAreEnums(t *testing.T) {
	boom := errors.New("disk I/O error: " + injection)
	tests := []struct {
		tool values.Tool
		args string
		fail func(r *reads)
	}{
		{values.ToolListSubjects, `{}`, func(r *reads) { r.library.err = boom }},
		{values.ToolFindLessons, `{}`, func(r *reads) { r.knowledge.err = boom }},
		{values.ToolFindLessons, `{"subject":"s1"}`, func(r *reads) { r.activity.err = boom }},
		{values.ToolListAssignments, `{}`, func(r *reads) { r.assignments.err = boom }},
		{values.ToolListAssignments, `{}`, func(r *reads) { r.library.err = boom }},
		{values.ToolJobStatus, `{"ids":["j1"]}`, func(r *reads) { r.activity.err = boom }},
		{values.ToolDeliveryStatus, `{"assignment":"a1"}`, func(r *reads) { r.deliveries.err = boom }},
		{values.ToolSearch, `{"query":"ряд"}`, func(r *reads) { r.knowledge.err = boom }},
	}
	for _, tt := range tests {
		r := newReads()
		tt.fail(r)
		out := r.tools().read(t.Context(), values.Scope{}, readCall(t, "c1", tt.tool, tt.args))
		if !out.Result.IsError || out.Result.ResultJSON != `{"error":"failed"}` || len(out.Seen) != 0 {
			t.Errorf("%s: %+v", tt.tool, out)
		}
	}
}
