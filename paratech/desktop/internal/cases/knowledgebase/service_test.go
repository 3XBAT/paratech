package knowledgebase

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

func TestNewRequiresEveryPort(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New without ports = %v, want ErrInvalidConfig", err)
	}
}

// The correction accepts the codes of the schedule's kinds (two contexts keep the same codes, ARCH-11).
func TestCorrectionKindsAreTheScheduleKinds(t *testing.T) {
	var codes []string
	for _, k := range lesson.Kinds() {
		if k != lesson.KindUnknown {
			codes = append(codes, k.String())
		}
	}
	if got := values.LessonKindCodes(); !reflect.DeepEqual(got, codes) {
		t.Fatalf("LessonKindCodes = %v, schedule kinds %v", got, codes)
	}
}

func recIDs(ls []kb.Lesson) []shared.RecordingID {
	var out []shared.RecordingID
	for _, l := range ls {
		out = append(out, l.Recording)
	}
	return out
}

func TestSubjectsAndLessons(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.add(t, lessonSpec{id: "r1", title: "Механика", subject: "Физика", kind: "lecture"})
	e.add(t, lessonSpec{id: "r2", start: 24 * time.Hour, title: "Оптика", subject: "физика", kind: "seminar"})
	e.add(t, lessonSpec{id: "r3", start: time.Hour, title: "Live", subject: "Химия", kind: "lab", live: true})
	e.add(t, lessonSpec{id: "r4", start: 2 * time.Hour}) // ad hoc
	e.addTranscript(t, "r1", " раз")
	e.addSummary(t, "r1", "Кратко.")

	// Not in the base: silent, marked for deletion, failed.
	silent, err := recording.New(recording.Spec{ID: "silent", MaxDuration: time.Hour}, t0)
	if err != nil {
		t.Fatal(err)
	}
	track, _ := silent.TrackRef("wav")
	if err := errors.Join(silent.Begin(t0), silent.Stop(t0.Add(time.Hour)), silent.Finalize(t0.Add(time.Hour)),
		silent.CompleteTrack(t0.Add(time.Hour), track, values.AssembledTrack{Duration: time.Hour, Seconds: 3600, AudibleSeconds: 1})); err != nil {
		t.Fatal(err)
	}
	e.recs.rows["silent"] = silent
	deleting := e.add(t, lessonSpec{id: "deleting", title: "X", subject: "Физика"})
	if err := deleting.MarkDeleting(t0.Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	failed, _ := recording.New(recording.Spec{ID: "failed", MaxDuration: time.Hour}, t0)
	if err := failed.Fail(t0, "device not found"); err != nil {
		t.Fatal(err)
	}
	e.recs.rows["failed"] = failed

	subjects, err := e.svc.Subjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []kb.Subject{
		{Key: "физика", Name: "физика", Lessons: 2, Latest: t0.Add(24 * time.Hour)},
		{Key: "химия", Name: "Химия", Lessons: 1, Latest: t0.Add(time.Hour)},
		{Key: "", Name: kb.NoSubjectName, Lessons: 1, Latest: t0.Add(2 * time.Hour), NoSubject: true},
	}
	if !reflect.DeepEqual(subjects, want) {
		t.Fatalf("Subjects =\n %+v\nwant\n %+v", subjects, want)
	}

	physics, err := e.svc.Lessons(ctx, "Физика")
	if err != nil || !reflect.DeepEqual(recIDs(physics), []shared.RecordingID{"r2", "r1"}) {
		t.Fatalf("Lessons(Физика) = %v, %v", recIDs(physics), err)
	}
	r1 := physics[1]
	if r1.Title != "Механика" || r1.Kind != "lecture" || r1.Duration != 90*time.Minute ||
		r1.Ready != (kb.Readiness{Transcript: "tr-r1", Summary: true}) {
		t.Fatalf("r1 = %+v", r1)
	}

	chem, err := e.svc.Lessons(ctx, "химия")
	if err != nil || len(chem) != 1 || chem[0].Title != "Live" || chem[0].Kind != "lab" {
		t.Fatalf("Lessons(химия) = %+v, %v (the live lesson names a recording without a snapshot)", chem, err)
	}
	if none, err := e.svc.Lessons(ctx, ""); err != nil || !reflect.DeepEqual(recIDs(none), []shared.RecordingID{"r4"}) {
		t.Fatalf("Lessons(no subject) = %v, %v", recIDs(none), err)
	}
	if got, err := e.svc.Lessons(ctx, "История"); err != nil || len(got) != 0 {
		t.Fatalf("Lessons(unknown) = %v, %v", got, err)
	}
}

func TestListFailures(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.add(t, lessonSpec{id: "r1", title: "Механика", subject: "Физика"})
	e.recs.unreadable = []shared.RecordingID{"broken"}
	if subjects, err := e.svc.Subjects(ctx); err != nil || len(subjects) != 1 {
		t.Fatalf("Subjects with an unreadable row = %+v, %v", subjects, err)
	}
	if !strings.Contains(e.logs.String(), "broken") {
		t.Fatalf("the unreadable row is not logged: %s", e.logs)
	}

	e.recs.listErr = errDisk
	if _, err := e.svc.Subjects(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Subjects = %v, want ErrUnavailable", err)
	}
}

func TestLesson(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.add(t, lessonSpec{id: "r1", title: "Механика", subject: "Физика"})
	failed, _ := recording.New(recording.Spec{ID: "failed", MaxDuration: time.Hour}, t0)
	_ = failed.Fail(t0, "device not found")
	e.recs.rows["failed"] = failed

	if l, err := e.svc.Lesson(ctx, "r1"); err != nil || l.Subject != "Физика" || l.Title != "Механика" {
		t.Fatalf("Lesson = %+v, %v", l, err)
	}
	for id, want := range map[shared.RecordingID]error{"nope": ErrRecordingNotFound, "failed": ErrRecordingNotFound, "a/b": ErrInvalidRequest} {
		if _, err := e.svc.Lesson(ctx, id); !errors.Is(err, want) {
			t.Errorf("Lesson(%s) = %v, want %v", id, err, want)
		}
	}
}

func TestSetLessonInfo(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	rec := e.add(t, lessonSpec{id: "r1", title: "Механика", subject: "Физика", kind: "lecture"})
	snapshot := rec.LessonSnapshot()

	l, err := e.svc.SetLessonInfo(ctx, "r1", " Теормех ", "Seminar")
	if err != nil || l.Subject != "Теормех" || l.Kind != "seminar" || !l.SubjectEdited || !l.KindEdited || l.Title != "Механика" {
		t.Fatalf("SetLessonInfo = %+v, %v", l, err)
	}
	if e.recs.saves != 1 || e.recs.rows["r1"].LessonSnapshot() != snapshot {
		t.Fatalf("saves %d, snapshot %+v; want one save and the snapshot as recorded", e.recs.saves, e.recs.rows["r1"].LessonSnapshot())
	}
	if subjects, _ := e.svc.Subjects(ctx); len(subjects) != 1 || subjects[0].Name != "Теормех" {
		t.Fatalf("Subjects after the correction = %+v", subjects)
	}

	// The same again saves nothing; empty clears it and the snapshot names the lesson again.
	if _, err := e.svc.SetLessonInfo(ctx, "r1", "Теормех", "seminar"); err != nil || e.recs.saves != 1 {
		t.Fatalf("same correction: %v, saves %d", err, e.recs.saves)
	}
	l, err = e.svc.SetLessonInfo(ctx, "r1", "", "")
	if err != nil || l.Subject != "Физика" || l.Kind != "lecture" || l.SubjectEdited || l.KindEdited {
		t.Fatalf("cleared = %+v, %v", l, err)
	}

	// A lost save is tried again, up to correctAttempts.
	e.recs.conflicts = correctAttempts - 1
	if _, err := e.svc.SetLessonInfo(ctx, "r1", "Химия", ""); err != nil {
		t.Fatalf("after conflicts: %v", err)
	}
	e.recs.conflicts = correctAttempts
	if _, err := e.svc.SetLessonInfo(ctx, "r1", "Биология", ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("always conflicting: %v, want ErrBusy", err)
	}
	e.recs.conflicts = 0
	e.recs.saveErr = errDisk
	if _, err := e.svc.SetLessonInfo(ctx, "r1", "Биология", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("disk error: %v, want ErrUnavailable", err)
	}
}

func TestSetLessonInfoRefuses(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.add(t, lessonSpec{id: "r1"})
	deleting := e.add(t, lessonSpec{id: "deleting"})
	if err := deleting.MarkDeleting(t0.Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	running, _ := recording.New(recording.Spec{ID: "running", MaxDuration: time.Hour}, t0)
	_ = running.Begin(t0)
	e.recs.rows["running"] = running
	failed, _ := recording.New(recording.Spec{ID: "failed", MaxDuration: time.Hour}, t0)
	_ = failed.Fail(t0, "device not found")
	e.recs.rows["failed"] = failed
	silent, _ := recording.New(recording.Spec{ID: "silent", MaxDuration: time.Hour}, t0)
	track, _ := silent.TrackRef("wav")
	if err := errors.Join(silent.Begin(t0), silent.Stop(t0.Add(time.Hour)), silent.Finalize(t0.Add(time.Hour)),
		silent.CompleteTrack(t0.Add(time.Hour), track, values.AssembledTrack{Duration: time.Hour, Seconds: 3600, AudibleSeconds: 1})); err != nil {
		t.Fatal(err)
	}
	e.recs.rows["silent"] = silent

	tests := []struct {
		id            shared.RecordingID
		subject, kind string
		want          error
	}{
		{"r1", strings.Repeat("ф", 200), "", ErrInvalidLessonInfo},
		{"r1", "Физика", "party", ErrInvalidLessonInfo},
		{"a/b", "Физика", "", ErrInvalidRequest},
		{"nope", "Физика", "", ErrRecordingNotFound},
		{"deleting", "Физика", "", ErrRecordingNotFound},
		{"running", "Физика", "", ErrNotFinished},
		{"failed", "Физика", "", ErrRecordingNotFound},
		{"silent", "Физика", "", ErrRecordingNotFound},
	}
	for _, tc := range tests {
		if _, err := e.svc.SetLessonInfo(ctx, tc.id, tc.subject, tc.kind); !errors.Is(err, tc.want) {
			t.Errorf("SetLessonInfo(%s, %q, %q) = %v, want %v", tc.id, tc.subject, tc.kind, err, tc.want)
		}
	}
	if e.recs.saves != 0 {
		t.Fatalf("%d saves of refused corrections", e.recs.saves)
	}
}

func TestSearch(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.add(t, lessonSpec{id: "old", title: "Квантовая механика", subject: "Физика"})
	e.add(t, lessonSpec{id: "new", start: 24 * time.Hour, title: "Оптика", subject: "Физика"})
	e.addTranscript(t, "old", " Сегодня про кванты.", " Ещё про <b>КВАНТЫ</b>.")
	e.addSummary(t, "old", "Про кванты и поля.", "Кванты дискретны")
	e.addLectureText(t, "old", block(t, "Кванты", 60_000, "Энергия квантуется."))
	e.addTranscript(t, "new", " Свет и линзы.")

	res, err := e.svc.Search(ctx, "квант")
	if err != nil {
		t.Fatal(err)
	}
	type place struct {
		rec   shared.RecordingID
		sec   kb.Section
		index int
	}
	var got []place
	for _, h := range res.Hits {
		got = append(got, place{h.Recording, h.Section, h.Index})
		if h.Lesson.Recording != h.Recording || h.Lesson.Subject != "Физика" {
			t.Errorf("hit %+v carries the lesson %+v", h.Hit, h.Lesson)
		}
	}
	want := []place{
		{"old", kb.SectionTitle, 0},
		{"old", kb.SectionSummary, 0}, {"old", kb.SectionSummary, 1},
		{"old", kb.SectionLectureText, 0},
		{"old", kb.SectionTranscript, 0}, {"old", kb.SectionTranscript, 1},
	}
	if !reflect.DeepEqual(got, want) || res.Truncated {
		t.Fatalf("hits = %v (truncated %v)\nwant %v", got, res.Truncated, want)
	}
	if h := res.Hits[5]; h.StartMs != 1000 || h.Snippet.Match != "КВАНТ" || h.Snippet.Before != "Ещё про <b>" {
		t.Fatalf("transcript hit = %+v (model and speech text stays plain text)", h)
	}
	if h := res.Hits[3]; h.StartMs != 60_000 {
		t.Fatalf("lecture text hit = %+v", h)
	}

	if res, err := e.svc.Search(ctx, "линз"); err != nil || len(res.Hits) != 1 || res.Hits[0].Recording != "new" {
		t.Fatalf("Search(линз) = %+v, %v", res, err)
	}
	if res, err := e.svc.Search(ctx, "нет такого"); err != nil || res.Hits == nil || len(res.Hits) != 0 {
		t.Fatalf("Search(nothing) = %+v, %v; want an empty list", res, err)
	}
	if _, err := e.svc.Search(ctx, "к"); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("short query: %v", err)
	}
}

// Deleted recordings leave the base by themselves: a marked one is skipped, a purged one is gone (ADR-0019).
func TestSearchSkipsDeletedRecordings(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	rec := e.add(t, lessonSpec{id: "r1", title: "Кванты", subject: "Физика"})
	e.addTranscript(t, "r1", " кванты")
	if res, _ := e.svc.Search(ctx, "кванты"); len(res.Hits) != 2 {
		t.Fatalf("before the deletion: %+v", res.Hits)
	}

	if err := rec.MarkDeleting(t0.Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if res, err := e.svc.Search(ctx, "кванты"); err != nil || len(res.Hits) != 0 {
		t.Fatalf("marked for deletion: %+v, %v", res.Hits, err)
	}
	if subjects, _ := e.svc.Subjects(ctx); len(subjects) != 0 {
		t.Fatalf("marked for deletion is listed: %+v", subjects)
	}
}

func TestSearchSurvivesBrokenTranscripts(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.add(t, lessonSpec{id: "r1", title: "Кванты", subject: "Физика"})
	id := e.addTranscript(t, "r1", " кванты")
	ref := e.transcripts[id].Text()
	if err := e.artifacts.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.Search(ctx, "кванты")
	if err != nil || len(res.Hits) != 1 || res.Hits[0].Section != kb.SectionTitle {
		t.Fatalf("missing file: %+v, %v; want the title hit only", res.Hits, err)
	}
	if !strings.Contains(e.logs.String(), "transcript not searched") {
		t.Fatalf("the missing file is not logged: %s", e.logs)
	}
}

// A summary or lecture text row that cannot be read (say, it fails the aggregate's restore) costs only that section;
// a cancelled or timed out read still fails the search.
func TestSearchSurvivesBrokenRows(t *testing.T) {
	tests := []struct {
		name     string
		summary  error
		lecture  error
		sections []kb.Section
		want     error
		log      string
	}{
		{name: "broken summary", summary: errDisk, log: "summary not searched",
			sections: []kb.Section{kb.SectionTitle, kb.SectionLectureText, kb.SectionTranscript}},
		{name: "broken lecture text", lecture: errDisk, log: "lecture text not searched",
			sections: []kb.Section{kb.SectionTitle, kb.SectionSummary, kb.SectionTranscript}},
		{name: "cancelled summary read", summary: context.Canceled, want: context.Canceled},
		{name: "timed out lecture text read", lecture: fmt.Errorf("query: %w", context.DeadlineExceeded), want: context.DeadlineExceeded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, ctx := newEnv(t), context.Background()
			e.add(t, lessonSpec{id: "r1", title: "Кванты", subject: "Физика"})
			e.addTranscript(t, "r1", " кванты")
			e.addSummary(t, "r1", "Секрет кванты.")
			e.addLectureText(t, "r1", block(t, "Тайна", 0, "Тайна кванты."))
			e.summaries.err, e.lectures.err = tc.summary, tc.lecture

			res, err := e.svc.Search(ctx, "кванты")
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("Search = %v, want %v", err, tc.want)
				}
				return
			}
			var got []kb.Section
			for _, h := range res.Hits {
				got = append(got, h.Section)
			}
			if err != nil || !reflect.DeepEqual(got, tc.sections) {
				t.Fatalf("sections %v, %v; want %v", got, err, tc.sections)
			}
			logs := e.logs.String()
			if !strings.Contains(logs, tc.log) || !strings.Contains(logs, "r1") || strings.Contains(logs, "Секрет") ||
				strings.Contains(logs, "Тайна") {
				t.Fatalf("logs = %s; want %q with the recording and no text", logs, tc.log)
			}
		})
	}
}

func TestSearchStopsWhenFull(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	for i := range kb.MaxHits + 5 {
		id := shared.RecordingID(fmt.Sprintf("r%03d", i))
		e.add(t, lessonSpec{id: id, start: time.Duration(i) * time.Hour, title: "Кванты", subject: "Физика"})
	}
	res, err := e.svc.Search(ctx, "кванты")
	if err != nil || len(res.Hits) != kb.MaxHits || !res.Truncated {
		t.Fatalf("hits %d, truncated %v, %v", len(res.Hits), res.Truncated, err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := e.svc.Search(cancelled, "кванты"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search: %v", err)
	}
}

func TestExportSubject(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.add(t, lessonSpec{id: "late", start: 24 * time.Hour, title: "Оптика", subject: "Физика", kind: "seminar"})
	e.add(t, lessonSpec{id: "early", title: "Механика", subject: "физика", kind: "lecture"})
	e.add(t, lessonSpec{id: "other", title: "Реакции", subject: "Химия"})
	e.addTranscript(t, "early", " раз")
	e.addSummary(t, "early", "Законы Ньютона.", "F = ma")
	e.addLectureText(t, "early", block(t, "Введение", 65_000, "Начнём."))

	name, err := e.svc.SubjectFileName(ctx, "ФИЗИКА")
	if err != nil || name != "Физика.md" {
		t.Fatalf("SubjectFileName = %q, %v (the newest lesson spells the subject)", name, err)
	}
	if err := e.svc.ExportSubject(ctx, "физика", `C:\out\Физика.md`); err != nil {
		t.Fatal(err)
	}
	want := "# Физика\n" +
		"\n## 19\\.10.2026, 10:00 · лекция · Механика\n\n1 ч 30 мин\n\n" +
		"### Кратко\n\nЗаконы Ньютона.\n\n#### Ключевые моменты\n\n- F = ma\n" +
		"\n### Текст лекции\n\n#### \\[01:05\\] Введение\n\nНачнём.\n" +
		"\n## 20\\.10.2026, 10:00 · семинар · Оптика\n\n1 ч 30 мин\n\n" +
		"### Кратко\n\nКонспекта пока нет.\n\n### Текст лекции\n\nТекста лекции пока нет.\n"
	if e.writer.path != `C:\out\Физика.md` || e.writer.content != want {
		t.Fatalf("wrote %q:\n%s\nwant\n%s", e.writer.path, e.writer.content, want)
	}

	if none, err := e.svc.SubjectFileName(ctx, ""); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("SubjectFileName(no subject) = %q, %v", none, err)
	}
	if err := e.svc.ExportSubject(ctx, "История", "x.md"); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("unknown subject: %v", err)
	}
	if err := e.svc.ExportSubject(ctx, "Физика", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("no path: %v", err)
	}
	e.writer.err = errDisk
	if err := e.svc.ExportSubject(ctx, "Физика", "x.md"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("write error: %v", err)
	}
}

func TestExportWithoutSubject(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.add(t, lessonSpec{id: "r1"})
	name, err := e.svc.SubjectFileName(ctx, "")
	if err != nil || name != kb.NoSubjectName+".md" {
		t.Fatalf("SubjectFileName = %q, %v", name, err)
	}
	if err := e.svc.ExportSubject(ctx, "", "x.md"); err != nil || !strings.HasPrefix(e.writer.content, "# "+kb.NoSubjectName+"\n") ||
		!strings.Contains(e.writer.content, "· Запись\n") {
		t.Fatalf("export = %v:\n%s", err, e.writer.content)
	}
}
