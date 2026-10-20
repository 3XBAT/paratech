package summaryview

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// addLecture stores a lecture text of tr: published blocks when ready, and a run of 12 parts with 3 done.
func (e *env) addLecture(t *testing.T, tr shared.TranscriptID, ready bool) {
	t.Helper()
	l, err := lecturetext.New(lecturetext.Spec{Transcript: tr}, t0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := lecturetext.NewBlock("Введение", 65_000, []string{"Сегодня говорим о реках.", "<b>Важно</b>"})
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		if err := errors.Join(l.AddPart("r1", 1, 0, []lecturetext.Block{b}, t0),
			l.Publish("r1", []lecturetext.Block{b}, 1, t0.Add(2*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	e.lectures.rows[l.ID()] = l
}

// lectureJob stores the lecture_text job of tr in status st with the part checkpoint, if any.
func (e *env) lectureJob(t *testing.T, tr shared.TranscriptID, st job.Status, checkpoint string) {
	t.Helper()
	e.addJob(t, "lecture_text", tr, job.StatusQueued, job.CauseNone)
	j := e.jobs.byPrefix["lecture_text:"+string(tr)+":"]
	if st == job.StatusQueued && checkpoint == "" {
		return
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(j.Claim(t0))
	if checkpoint != "" {
		p, err := job.NewProgress(25, checkpoint)
		must(err)
		must(j.ReportProgress(p, t0))
	}
	switch st {
	case job.StatusFailed:
		must(j.Fail(t0, "post-processing: the LLM answer does not match the schema"))
	case job.StatusCompleted:
		must(j.Complete(t0))
	case job.StatusRetryWait:
		must(j.Retry(t0, t0.Add(time.Minute), "hub timeout"))
	case job.StatusQueued: // a step stored its part and yielded (TASK-097)
		must(j.Yield(t0))
	}
}

func TestViewLectureText(t *testing.T) {
	tests := []struct {
		name       string
		ready      bool
		status     job.Status // "": no job
		checkpoint string
		want       LectureText // without the blocks
	}{
		{name: "never made", want: LectureText{Step: Step{State: StateNone}, CanStart: true}},
		{name: "made before, no job", ready: true, want: LectureText{Step: Step{State: StateDone}, Ready: true}},
		{name: "queued", status: job.StatusQueued, want: LectureText{Step: Step{State: StateWaiting, Wait: WaitInLine}}},
		{
			name: "running with parts", status: job.StatusRunning, checkpoint: "3/12",
			want: LectureText{Step: Step{State: StateRunning}, PartsDone: 3, Parts: 12},
		},
		{
			// TASK-097: between two steps it is still being made, not «в очереди».
			name: "between parts", status: job.StatusQueued, checkpoint: "3/12",
			want: LectureText{Step: Step{State: StateRunning}, PartsDone: 3, Parts: 12},
		},
		{
			name: "queued before any part", status: job.StatusQueued, checkpoint: "0/12",
			want: LectureText{Step: Step{State: StateWaiting, Wait: WaitInLine}, PartsDone: 0, Parts: 12},
		},
		{
			name: "retrying with parts", status: job.StatusRetryWait, checkpoint: "4/12",
			want: LectureText{Step: Step{State: StateWaiting, Wait: WaitRetry, JobError: "hub timeout"}, PartsDone: 4, Parts: 12},
		},
		{
			name: "failed, the text before stays", ready: true, status: job.StatusFailed, checkpoint: "4/12",
			want: LectureText{
				Step:  Step{State: StateFailed, JobError: "post-processing: the LLM answer does not match the schema"},
				Ready: true,
			},
		},
		{name: "done", ready: true, status: job.StatusCompleted, want: LectureText{Step: Step{State: StateDone}, Ready: true}},
		{name: "done but removed", status: job.StatusCompleted, want: LectureText{Step: Step{State: StateNone}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.addRecording(t, "r1", "География")
			tr := e.addTranscript(t, "r1")
			if tc.ready {
				e.addLecture(t, tr, true)
			}
			if tc.status != "" {
				e.lectureJob(t, tr, tc.status, tc.checkpoint)
			}
			v, err := e.svc.View(context.Background(), "r1")
			if err != nil {
				t.Fatal(err)
			}
			got := v.LectureText
			if got.Ready != (len(got.Blocks) > 0) || (got.Ready && !got.UpdatedAt.Equal(t0.Add(2*time.Hour))) {
				t.Fatalf("blocks %d, updated %v", len(got.Blocks), got.UpdatedAt)
			}
			got.Blocks, got.UpdatedAt = nil, time.Time{}
			if got.Step != tc.want.Step || got.Ready != tc.want.Ready || got.PartsDone != tc.want.PartsDone ||
				got.Parts != tc.want.Parts || got.CanStart != tc.want.CanStart {
				t.Fatalf("lecture text = %+v, want %+v", got, tc.want)
			}
		})
	}

	// Before the transcription there is nothing to start.
	e := newEnv(t)
	e.addRecording(t, "r1", "")
	v, err := e.svc.View(context.Background(), "r1")
	if err != nil || v.LectureText.State != StateNone || v.LectureText.CanStart {
		t.Fatalf("before transcription: %+v, %v", v.LectureText, err)
	}
	// An unreadable lecture text is a storage failure.
	e.addTranscript(t, "r1")
	e.lectures.err = errDisk
	if _, err := e.svc.View(context.Background(), "r1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("View = %v, want ErrUnavailable", err)
	}
}

func TestLectureTextCopyAndExport(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "География")
	tr := e.addTranscript(t, "r1")
	ctx := context.Background()
	if _, err := e.svc.LectureText(ctx, "r1", false); !errors.Is(err, ErrNoLectureText) {
		t.Fatalf("no lecture text: %v", err)
	}
	e.addLecture(t, tr, false) // a run in progress only
	if _, err := e.svc.LectureText(ctx, "r1", false); !errors.Is(err, ErrNoLectureText) {
		t.Fatalf("unfinished lecture text: %v", err)
	}
	e.addLecture(t, tr, true)

	got, err := e.svc.LectureText(ctx, "r1", true)
	if err != nil {
		t.Fatal(err)
	}
	want := "География\n19.10.2026, 12:05 · 1 ч 25 мин\n\n[01:05] Введение\n\nСегодня говорим о реках.\n\n<b>Важно</b>\n"
	if got != want {
		t.Fatalf("LectureText =\n%q\nwant\n%q", got, want)
	}

	path := `C:\Users\student\лекция.md`
	if err := e.svc.ExportLectureText(ctx, "r1", summaryexport.FormatMarkdown, false, path); err != nil {
		t.Fatal(err)
	}
	wantMD := "# География\n\n19\\.10.2026, 12:05 · 1 ч 25 мин\n\n## Введение\n\nСегодня говорим о реках.\n\n\\<b\\>Важно\\</b\\>\n"
	if e.writer.path != path || e.writer.content != wantMD {
		t.Fatalf("wrote %q to %q, want %q", e.writer.content, e.writer.path, wantMD)
	}

	for name, tc := range map[string]struct {
		f    summaryexport.Format
		path string
		want error
	}{
		"unknown format": {"html", path, ErrInvalidRequest},
		"no path":        {summaryexport.FormatText, "", ErrInvalidRequest},
	} {
		if err := e.svc.ExportLectureText(ctx, "r1", tc.f, false, tc.path); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	e.writer.err = errDisk
	if err := e.svc.ExportLectureText(ctx, "r1", summaryexport.FormatText, false, path); !errors.Is(err, ErrUnavailable) {
		t.Errorf("write failed: %v", err)
	}
}

func TestLectureSuggestedName(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", `Физика: 9/А`)
	e.addRecording(t, "r2", "")
	tests := []struct {
		id   shared.RecordingID
		f    summaryexport.Format
		want string
	}{
		{"r1", summaryexport.FormatMarkdown, "Физика_ 9_А — текст лекции 2026-10-19.md"},
		{"r2", summaryexport.FormatText, "Текст лекции 2026-10-19 12-05.txt"},
	}
	for _, tc := range tests {
		got, err := e.svc.LectureSuggestedName(context.Background(), tc.id, tc.f)
		if err != nil || got != tc.want {
			t.Errorf("LectureSuggestedName(%s) = %q, %v; want %q", tc.id, got, err, tc.want)
		}
	}
	if _, err := e.svc.LectureSuggestedName(context.Background(), "r1", "html"); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("unknown format: %v", err)
	}
	if _, err := e.svc.LectureSuggestedName(context.Background(), "nope", summaryexport.FormatText); !errors.Is(err, ErrRecordingNotFound) {
		t.Errorf("missing recording: %v", err)
	}
}
