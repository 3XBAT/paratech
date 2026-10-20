package homeworkflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/recording/recording"
	recvalues "paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// TASK-093: every operation on the transcript gets the lesson context with the next lesson of the same subject.

const week = 7 * 24 * time.Hour

func (e *ppEnv) addLesson(id shared.LessonID, title, subject, teacher string, kind lesson.Kind, start time.Time, deleted bool) {
	e.t.Helper()
	l, err := lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
		Title: title, Subject: subject, Kind: kind, Teacher: teacher, Start: start.UTC(), End: start.Add(90 * time.Minute).UTC(),
		TimeZone: "Europe/Moscow",
	}}, ppT0)
	if err != nil {
		e.t.Fatal(err)
	}
	if deleted {
		if err := l.MarkDeleted(ppT0); err != nil {
			e.t.Fatal(err)
		}
	}
	e.lessons[id] = l
}

func TestPostProcessingSendsTheNextLessonOfTheSubject(t *testing.T) {
	ops := map[job.Type]struct {
		op     values.Operation
		answer string
	}{
		SummarizeJobType:       {values.OperationSummary, `{"sections":[{"heading":"H","text":"S","points":[]}],"key_points":[]}`},
		ExtractHomeworkJobType: {values.OperationExtractHomework, `{"assignments":[],"subject_hint":"","teacher_hint":""}`},
		LectureTextJobType:     {values.OperationLectureText, lectureAnswer("Rivers", 0, "Today we talk about rivers. Homework: read chapter 4 by Friday.")},
	}
	for typ, o := range ops {
		t.Run(string(typ), func(t *testing.T) {
			e := newPPEnv(t)
			// l1 "Geography" on Wednesday 10:00 MSK and "next" have no subject column: the subject comes from the title.
			e.addLesson("next", "Geography, seminar", "", "Тестов", lesson.KindSeminar, lessonStart.Add(week), false)
			e.addLesson("later", "Geography", "Geography", "", lesson.KindLecture, lessonStart.Add(2*week), false)
			e.addLesson("other", "Physics", "Physics", "", lesson.KindUnknown, lessonStart.Add(24*time.Hour), false)
			e.addLesson("gone", "Geography", "Geography", "", lesson.KindUnknown, lessonStart.Add(48*time.Hour), true)
			e.llm.Answer(o.op, o.answer)

			if err := e.pp.Handle(context.Background(), e.snapshot("j1", typ, "t1"), (&reports{}).report); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			reqs := e.llm.Requests()
			if len(reqs) != 1 {
				t.Fatalf("LLM calls = %d", len(reqs))
			}
			data := untrusted(t, reqs[0])
			if data["next_lesson_start"] != "2026-10-21T10:00:00+03:00" || data["next_lesson_weekday"] != "Wednesday" || data["next_lesson_kind"] != "seminar" ||
				data["lesson_start"] != "2026-10-14T10:00:00+03:00" || data["lesson_title"] != "Geography" {
				t.Fatalf("untrusted data = %v", data)
			}
			if strings.Contains(reqs[0].Instructions(), "2026-10-21") {
				t.Fatal("the next lesson is in the instructions")
			}
		})
	}
}

// The recording's snapshot names the subject and the teacher even when its lesson left the schedule.
func TestNextLessonFromTheSnapshotSubject(t *testing.T) {
	e := newPPEnv(t)
	kept := lessonStart.Add(-time.Hour)
	e.addTranscript("r2", "gone", "t2", kept.Add(time.Minute), "Задачи 1–5 к следующему занятию.")
	r, err := recording.New(recording.Spec{
		ID: "r2", LessonID: "gone", MaxDuration: 3 * time.Hour,
		Lesson: recvalues.NewLessonSnapshot(recvalues.LessonSnapshotSpec{
			Title: "Тестовый анализ, практика", Subject: "Тестовый анализ", Kind: "practice", Teacher: "Вымыслов В.В.",
			Start: kept.UTC(), End: kept.Add(90 * time.Minute).UTC(),
		}),
	}, kept.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	e.recordings["r2"] = r
	e.addLesson("next", "Тестовый анализ (лек.)", "тестовый анализ", "", lesson.KindLecture, kept.Add(3*24*time.Hour), false)
	e.llm.Answer(values.OperationExtractHomework, `{"assignments":[{"title":"Задачи 1–5","description":"","due":"2026-10-17T09:00:00+03:00"}],"subject_hint":"","teacher_hint":""}`)

	if err := e.handle(e.snapshot("j1", ExtractHomeworkJobType, "t2")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	data := untrusted(t, e.llm.Requests()[0])
	want := map[string]string{
		"lesson_subject": "Тестовый анализ", "lesson_teacher": "Вымыслов В.В.", "lesson_kind": "practice",
		"lesson_start": "2026-10-14T09:00:00+03:00", "next_lesson_start": "2026-10-17T09:00:00+03:00",
		"next_lesson_kind": "lecture",
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("%s = %v, want %q", k, data[k], v)
		}
	}
	if got := e.assignmentsOf("t2"); len(got) != 1 {
		t.Fatalf("assignments = %+v", got)
	}
}

func TestNextLessonUnknown(t *testing.T) {
	t.Run("no lesson of the subject", func(t *testing.T) {
		e := newPPEnv(t)
		e.addLesson("other", "Physics", "Physics", "", lesson.KindUnknown, lessonStart.Add(24*time.Hour), false)
		e.llm.Answer(values.OperationSummary, `{"sections":[{"heading":"H","text":"S","points":[]}],"key_points":[]}`)
		if err := e.handle(e.snapshot("j1", SummarizeJobType, "t1")); err != nil {
			t.Fatalf("Handle: %v", err)
		}
		data := untrusted(t, e.llm.Requests()[0])
		if _, ok := data["next_lesson_start"]; ok || data["next_lesson_kind"] != nil || e.listCalls != 1 {
			t.Fatalf("next lesson sent or schedule not read (%d calls)", e.listCalls)
		}
	})
	t.Run("ad-hoc recording without a subject", func(t *testing.T) {
		e := newPPEnv(t)
		e.addTranscript("r2", "", "t2", lessonStart, "Homework: essay.")
		e.llm.Answer(values.OperationSummary, `{"sections":[{"heading":"H","text":"S","points":[]}],"key_points":[]}`)
		if err := e.handle(e.snapshot("j1", SummarizeJobType, "t2")); err != nil {
			t.Fatalf("Handle: %v", err)
		}
		if _, ok := untrusted(t, e.llm.Requests()[0])["next_lesson_start"]; ok || e.listCalls != 0 {
			t.Fatalf("next lesson looked up for an unknown subject (%d calls)", e.listCalls)
		}
	})
}

// A schedule read error is retried like the other reads; nothing is asked of the LLM.
func TestNextLessonReadErrorIsRetried(t *testing.T) {
	e := newPPEnv(t)
	e.listErr = fmt.Errorf("disk: %w", shared.ErrStorage)
	err := e.handle(e.snapshot("j1", ExtractHomeworkJobType, "t1"))
	if !errors.Is(err, ErrStorage) || backoff.Classify(err) == backoff.ClassPermanent {
		t.Fatalf("Handle = %v, want a retryable storage error", err)
	}
	if n := len(e.llm.Requests()); n != 0 {
		t.Fatalf("LLM calls = %d", n)
	}
}

// The log names the prompts version of accepted and rejected answers, so that runs can be compared.
func TestPostProcessingLogsThePromptsVersion(t *testing.T) {
	e := newPPEnv(t)
	var buf bytes.Buffer
	d := e.deps()
	d.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	pp, err := NewPostProcessor(d)
	if err != nil {
		t.Fatal(err)
	}
	e.llm.Answer(values.OperationSummary, "not json")
	e.llm.Answer(values.OperationSummary, `{"sections":[{"heading":"H","text":"S","points":[]}],"key_points":[]}`)
	e.llm.Answer(values.OperationLectureText, lectureAnswer("Rivers", 0, "Today we talk about rivers. Homework: read chapter 4 by Friday."))

	if err := pp.Handle(context.Background(), e.snapshot("j1", SummarizeJobType, "t1"), nil); err != nil {
		t.Fatalf("summary: %v", err)
	}
	if err := pp.Handle(context.Background(), e.snapshot("j2", LectureTextJobType, "t1"), (&reports{}).report); err != nil {
		t.Fatalf("lecture text: %v", err)
	}
	version := fmt.Sprintf(`"prompts_version":%d`, prompts.Version)
	lecture := fmt.Sprintf(`"lecture_text_version":%d`, prompts.LectureTextVersion)
	var rejected, accepted, lectureAccepted bool
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		switch {
		case strings.Contains(line, "answer rejected") && strings.Contains(line, version):
			rejected = true
		case strings.Contains(line, "answer accepted") && strings.Contains(line, `"job":"j1"`) && strings.Contains(line, version):
			accepted = true
		case strings.Contains(line, "answer accepted") && strings.Contains(line, `"job":"j2"`) && strings.Contains(line, lecture):
			lectureAccepted = true
		}
	}
	if !rejected || !accepted || !lectureAccepted {
		t.Fatalf("log misses the versions (rejected %v, accepted %v, lecture %v):\n%s", rejected, accepted, lectureAccepted, buf.String())
	}
}

// A bump of a prompts version gives new job keys, so a new version makes new jobs instead of finding the old ones.
func TestJobKeysCarryThePromptsVersion(t *testing.T) {
	for typ, v := range map[job.Type]int{
		SummarizeJobType: prompts.Version, ExtractHomeworkJobType: prompts.Version, LectureTextJobType: prompts.LectureTextVersion,
	} {
		if got := jobKey(typ, "t1"); !strings.HasSuffix(got, fmt.Sprintf("v%d", v)) {
			t.Errorf("%s: key %q does not end with v%d", typ, got, v)
		}
	}
}

// The lesson context is left out of the run id of a lecture text: a schedule change between attempts (here a new next
// lesson) keeps the stored parts, so the first part is not paid for again (ADR-0017).
func TestLectureTextRunIgnoresTheLessonContext(t *testing.T) {
	e := newLectureEnv(t, 150)
	e.addLesson("next", "Geography", "", "", lesson.KindSeminar, lessonStart.Add(week), false)
	e.llm.Reply(values.OperationLectureText,
		testenv.LLMReply{Output: partAnswer("A", 0, 0, 70)}, testenv.LLMReply{Err: shared.ErrNetworkUnavailable})
	snap := e.snapshot("j1", LectureTextJobType, "t2")
	if err := e.handle(snap); !errors.Is(err, shared.ErrNetworkUnavailable) {
		t.Fatalf("first attempt = %v", err)
	}
	if _, _, done := e.lecture("t2").Run(); done != 1 {
		t.Fatalf("parts stored = %d, want 1", done)
	}

	// The schedule is re-imported: an earlier next lesson of another kind.
	e.addLesson("sooner", "Geography", "", "Иной", lesson.KindLab, lessonStart.Add(3*24*time.Hour), false)
	e.freshLLM()
	e.llm.Reply(values.OperationLectureText,
		testenv.LLMReply{Output: partAnswer("B", 70, 70, 140)}, testenv.LLMReply{Output: partAnswer("C", 140, 140, 150)})
	if err := e.handle(snap); err != nil {
		t.Fatalf("second attempt: %v", err)
	}
	reqs := e.llm.Requests()
	if len(reqs) != 2 {
		t.Fatalf("calls = %d, want 2: the stored part is asked again", len(reqs))
	}
	if d := untrusted(t, reqs[0]); d["next_lesson_kind"] != "lab" || d["next_lesson_start"] != "2026-10-17T10:00:00+03:00" {
		t.Fatalf("the request does not carry the new next lesson: %v", d)
	}
	// The published text starts with the part stored by the first attempt.
	if l := e.lecture("t2"); !l.Ready() || l.Blocks()[0].Heading() != "A" {
		t.Fatal("the part of the first attempt was dropped")
	}
}
