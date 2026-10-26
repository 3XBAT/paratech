package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// fakeTurnkey answers with view and err and records what it got.
type fakeTurnkey struct {
	view    turnkey.View
	err     error
	advice  string
	replies []work.Reply
	edit    work.Understanding
	src     work.Source
	outline []work.OutlineItem
	calls   []string
}

func (f *fakeTurnkey) Work(context.Context, shared.WorkID) (turnkey.View, error) {
	return f.view, f.err
}

func (f *fakeTurnkey) WorkOf(_ context.Context, src work.Source) (turnkey.View, error) {
	f.src = src
	return f.view, f.err
}

func (f *fakeTurnkey) AnswerQuestions(_ context.Context, _ shared.WorkID, r []work.Reply) (turnkey.View, error) {
	f.replies = r
	return f.view, f.err
}

func (f *fakeTurnkey) SkipQuestions(context.Context, shared.WorkID) (turnkey.View, error) {
	return f.view, f.err
}

func (f *fakeTurnkey) EditUnderstanding(_ context.Context, _ shared.WorkID, u work.Understanding) (turnkey.View, error) {
	f.edit = u
	return f.view, f.err
}

func (f *fakeTurnkey) ConfirmRequirements(context.Context, shared.WorkID) (turnkey.View, error) {
	return f.view, f.err
}

func (f *fakeTurnkey) Retry(context.Context, shared.WorkID) (turnkey.View, error) {
	return f.view, f.err
}

func (f *fakeTurnkey) AskStep(context.Context, shared.WorkID, int, string) (string, error) {
	return f.advice, f.err
}

func (f *fakeTurnkey) StartWriting(context.Context, shared.WorkID) (turnkey.WritingStarted, error) {
	f.calls = append(f.calls, "start")
	return turnkey.WritingStarted{View: f.view, Job: "j1", Created: true}, f.err
}

func (f *fakeTurnkey) ApproveOutline(context.Context, shared.WorkID) (turnkey.WritingStarted, error) {
	f.calls = append(f.calls, "approve")
	return turnkey.WritingStarted{View: f.view, Job: "j2", Created: true}, f.err
}

func (f *fakeTurnkey) EditOutline(_ context.Context, _ shared.WorkID, items []work.OutlineItem) (turnkey.View, error) {
	f.calls, f.outline = append(f.calls, "edit"), items
	return f.view, f.err
}

func (f *fakeTurnkey) EditOutlineAt(_ context.Context, _ shared.WorkID, version int64, items []work.OutlineItem) (turnkey.View, error) {
	f.calls, f.outline = append(f.calls, fmt.Sprintf("edit at %d", version)), items
	return f.view, f.err
}

func (f *fakeTurnkey) StopWriting(context.Context, shared.WorkID) (turnkey.View, error) {
	f.calls = append(f.calls, "stop")
	return f.view, f.err
}

func (f *fakeTurnkey) Regenerate(_ context.Context, _ shared.WorkID, number int, note string) (turnkey.WritingStarted, error) {
	f.calls = append(f.calls, fmt.Sprintf("regenerate %d %s", number, note))
	return turnkey.WritingStarted{View: f.view, Job: "j3", Created: true}, f.err
}

// writingWork is a work whose outline waits for the approval: section 1 written for the test of the mapping.
func writingWork() turnkey.View {
	return turnkey.View{
		ID: "w1", Source: work.Source{Assignment: "a1"}, Status: work.StatusWriting, Round: 1, Version: 7,
		Writing: &turnkey.WritingView{
			Step: work.StepApprove, Generation: 1, Topic: "Древняя Русь", Done: 1, Total: 2, Subject: "s1",
			Sections: []turnkey.SectionView{
				{Number: 1, Title: "Введение", Words: 200, Written: true, Paragraphs: []string{"Абзац."},
					Sources: []work.Reference{{Text: "Методичка.docx", Material: "m1"}, {Text: "Иванов И. И."}}},
				{Number: 2, Title: "Итоги", Words: 150},
			},
		},
	}
}

func TestTurnkeyWritingJSON(t *testing.T) {
	s := NewTurnkeyService(&fakeTurnkey{view: writingWork()}, discard())
	got, err := s.Work(t.Context(), "w1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got.Writing)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"step":"approve","generation":1,"topic":"Древняя Русь","sections":[` +
		`{"number":1,"title":"Введение","words":200,"note":"","written":true,"paragraphs":["Абзац."],` +
		`"sources":[{"text":"Методичка.docx","materialId":"m1","verify":false},{"text":"Иванов И. И.","materialId":"","verify":true}]},` +
		`{"number":2,"title":"Итоги","words":150,"note":"","written":false,"paragraphs":[],"sources":[]}],` +
		`"done":1,"total":2,"subjectId":"s1","materialId":"","result":"","job":"","active":false,"jobState":""}`
	if string(b) != want {
		t.Fatalf("JSON =\n%s\nwant\n%s", b, want)
	}
}

// The writing moves of the student reach the use case; those that start a job give its ID for the progress card.
func TestTurnkeyWritingCalls(t *testing.T) {
	f := &fakeTurnkey{view: writingWork()}
	s := NewTurnkeyService(f, discard())
	ctx := t.Context()
	for name, tt := range map[string]struct {
		call func() (TurnkeyWork, error)
		job  string
	}{
		"start":      {func() (TurnkeyWork, error) { return s.StartWriting(ctx, "w1") }, "j1"},
		"approve":    {func() (TurnkeyWork, error) { return s.ApproveOutline(ctx, "w1") }, "j2"},
		"regenerate": {func() (TurnkeyWork, error) { return s.Regenerate(ctx, "w1", 2, "Подробнее") }, "j3"},
		"edit": {func() (TurnkeyWork, error) {
			return s.EditOutline(ctx, "w1", []TurnkeyOutlineItem{{Number: 2, Title: "Итоги", Words: 150}, {Title: "Новый", Words: 100}})
		}, ""},
	} {
		v, err := tt.call()
		if err != nil || v.ID != "w1" || v.Writing == nil || v.Writing.Job != tt.job {
			t.Errorf("%s = %+v, %v", name, v.Writing, err)
		}
	}
	if len(f.outline) != 2 || f.outline[0] != (work.OutlineItem{Number: 2, Title: "Итоги", Words: 150}) || f.outline[1].Number != 0 {
		t.Fatalf("outline = %+v", f.outline)
	}
	if !slices.Contains(f.calls, "regenerate 2 Подробнее") {
		t.Fatalf("calls = %v", f.calls)
	}

	for _, tt := range []struct {
		err  error
		want error
	}{
		{turnkey.ErrWritingRunning, ErrTurnkeyWriting},
		{turnkey.ErrAlreadyWritten, ErrTurnkeyWritten},
		{fmt.Errorf("%w: the outline waits", turnkey.ErrNotWritable), ErrTurnkeyInvalid},
		{turnkey.ErrWritingRetries, ErrTurnkeyInvalid},
		{fmt.Errorf("%w: %w", turnkey.ErrNotFound, shared.ErrNotFound), ErrTurnkeyNotFound},
	} {
		f.err = tt.err
		if _, err := s.ApproveOutline(ctx, "w1"); !errors.Is(err, tt.want) {
			t.Errorf("%v: %v", tt.err, err)
		}
	}
}

// «Остановить» and the versioned plan edit reach the use case; a stale editor and a stop that did not go through are
// fixed texts.
func TestTurnkeyStopAndEditAt(t *testing.T) {
	f := &fakeTurnkey{view: writingWork()}
	s := NewTurnkeyService(f, discard())
	ctx := t.Context()
	if v, err := s.StopWriting(ctx, "w1"); err != nil || v.ID != "w1" || v.Writing == nil || v.Writing.Active {
		t.Fatalf("StopWriting = %+v, %v", v.Writing, err)
	}
	items := []TurnkeyOutlineItem{{Number: 1, Title: "Введение", Words: 200}}
	if v, err := s.EditOutlineAt(ctx, "w1", 7, items); err != nil || v.Version != 7 {
		t.Fatalf("EditOutlineAt = %+v, %v", v, err)
	}
	if !slices.Equal(f.calls, []string{"stop", "edit at 7"}) || len(f.outline) != 1 || f.outline[0].Title != "Введение" {
		t.Fatalf("calls = %v, outline = %+v", f.calls, f.outline)
	}

	for _, tt := range []struct {
		err  error
		call func() (TurnkeyWork, error)
		want error
	}{
		{fmt.Errorf("%w: version 8", turnkey.ErrChanged), func() (TurnkeyWork, error) { return s.EditOutlineAt(ctx, "w1", 7, items) }, ErrTurnkeyStale},
		{turnkey.ErrWritingRunning, func() (TurnkeyWork, error) { return s.EditOutlineAt(ctx, "w1", 7, items) }, ErrTurnkeyWriting},
		{fmt.Errorf("%w: running", turnkey.ErrNotStopped), func() (TurnkeyWork, error) { return s.StopWriting(ctx, "w1") }, ErrTurnkeyNotStopped},
		{fmt.Errorf("%w: %w", turnkey.ErrNotFound, shared.ErrNotFound), func() (TurnkeyWork, error) { return s.StopWriting(ctx, "w1") }, ErrTurnkeyNotFound},
		{turnkey.ErrUnavailable, func() (TurnkeyWork, error) { return s.StopWriting(ctx, "w1") }, ErrTurnkeyUnavailable},
	} {
		f.err = tt.err
		if _, err := tt.call(); !errors.Is(err, tt.want) {
			t.Errorf("%v: %v, want %v", tt.err, err, tt.want)
		}
	}
	if ErrTurnkeyStale.Error() != "Работа изменилась: обновите карточку." {
		t.Fatalf("stale text = %q", ErrTurnkeyStale)
	}
}

// Every read of a work tells whether its writing job runs and in which state.
func TestTurnkeyWritingJobState(t *testing.T) {
	for _, tt := range []struct {
		active bool
		status job.Status
		want   string
	}{
		{true, job.StatusQueued, "queued"},
		{true, job.StatusRunning, "running"},
		{true, job.StatusWaitingForNetwork, "waiting_network"},
		{true, job.StatusWaitingForQuota, "waiting_quota"},
		{true, job.StatusWaitingForPower, "waiting_power"},
		{true, job.StatusRetryWait, "retrying"},
		{false, "", ""},
		{true, job.StatusCompleted, ""}, // never active; shown as no job
	} {
		t.Run(string(tt.status), func(t *testing.T) {
			v := writingWork()
			v.Writing.Active, v.Writing.JobState, v.Writing.Job = tt.active, tt.status, "j9"
			s := NewTurnkeyService(&fakeTurnkey{view: v}, discard())
			got, err := s.Work(t.Context(), "w1")
			if err != nil {
				t.Fatal(err)
			}
			w := got.Writing
			wantJob := ""
			if tt.want != "" {
				wantJob = "j9"
			}
			if w.Active != (tt.want != "") || w.JobState != tt.want || w.Job != wantJob {
				t.Fatalf("writing = active %v %q job %q, want %q", w.Active, w.JobState, w.Job, tt.want)
			}
			b, err := json.Marshal(w)
			if err != nil {
				t.Fatal(err)
			}
			if tt.want != "" && !strings.Contains(string(b), `"active":true,"jobState":"`+tt.want+`"`) {
				t.Fatalf("JSON = %s", b)
			}
		})
	}
}

func helperView() turnkey.View {
	return turnkey.View{
		ID: "w1", Source: work.Source{Assignment: "a1"}, Status: work.StatusHelper, Round: 1, HasAnswer: true,
		Understanding: work.Understanding{WorkType: work.TypeLabReport, Requirements: work.Requirements{FileFormat: "dwg"}},
		Edited:        []work.Field{work.FieldFileFormat}, Feasibility: work.FeasibleImpossible,
		FeasibilityReason: "Чертёж в программе вуза",
		Helper:            &work.Helper{Steps: []string{"Открыть программу"}, Submit: "Файл .dwg", ReadyParts: []string{"Записка"}},
		Answered:          []turnkey.Answered{{Round: 1, Question: "Вариант?", Answer: "3", Default: true}},
		Version:           4,
	}
}

func TestTurnkeyWorkJSON(t *testing.T) {
	s := NewTurnkeyService(&fakeTurnkey{view: helperView()}, discard())
	got, err := s.Work(t.Context(), "w1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"w1","assignmentId":"a1","materialId":"","status":"helper","round":1,"questionRound":0,"questions":[],` +
		`"hasCard":true,"card":{"workType":"lab_report","volume":"","fileFormat":"dwg","formatting":"","variant":"",` +
		`"deadline":"","sources":""},"edited":["file_format"],"feasibility":"impossible",` +
		`"feasibilityReason":"Чертёж в программе вуза","partial":false,"assumptions":[],` +
		`"helper":{"steps":["Открыть программу"],"submit":"Файл .dwg","pitfalls":[],"readyParts":["Записка"],"placeholders":[]},` +
		`"answered":[{"round":1,"question":"Вариант?","answer":"3","default":true}],"writing":null,"version":4,"problem":"","message":""}`
	if string(b) != want {
		t.Fatalf("JSON =\n%s\nwant\n%s", b, want)
	}
}

func TestTurnkeyCallsPassTheStudentsInput(t *testing.T) {
	f := &fakeTurnkey{view: helperView()}
	s := NewTurnkeyService(f, discard())
	ctx := t.Context()
	if _, err := s.AnswerQuestions(ctx, "w1", []TurnkeyAnswer{{Question: 0, Option: 1}, {Question: 1, Text: "Тема"}}); err != nil {
		t.Fatal(err)
	}
	if len(f.replies) != 2 || f.replies[0] != (work.Reply{Question: 0, Option: 1}) || f.replies[1].Text != "Тема" {
		t.Fatalf("replies = %+v", f.replies)
	}
	if _, err := s.EditUnderstanding(ctx, "w1", TurnkeyCard{WorkType: "essay", Volume: "5 страниц"}); err != nil {
		t.Fatal(err)
	}
	if f.edit.WorkType != work.TypeEssay || f.edit.Requirements.Volume != "5 страниц" {
		t.Fatalf("edit = %+v", f.edit)
	}
	if _, err := s.WorkOfAssignment(ctx, "a7"); err != nil || f.src != (work.Source{Assignment: "a7"}) {
		t.Fatalf("WorkOfAssignment: %v, %+v", err, f.src)
	}
	for name, call := range map[string]func() (TurnkeyWork, error){
		"skip":    func() (TurnkeyWork, error) { return s.SkipQuestions(ctx, "w1") },
		"confirm": func() (TurnkeyWork, error) { return s.ConfirmRequirements(ctx, "w1") },
		"retry":   func() (TurnkeyWork, error) { return s.Retry(ctx, "w1") },
	} {
		if v, err := call(); err != nil || v.ID != "w1" {
			t.Errorf("%s = %+v, %v", name, v, err)
		}
	}
	f.advice = "Масштаб 1:100"
	if a, err := s.AskStep(ctx, "w1", 0, "Какой масштаб?"); err != nil || a.Text != "Масштаб 1:100" {
		t.Fatalf("AskStep = %+v, %v", a, err)
	}
}

// A reader call that did not go through keeps the work: the page gets it with a problem, not an error.
func TestTurnkeyProblemsAndErrors(t *testing.T) {
	pending := helperView()
	pending.Status = work.StatusTriage
	tests := []struct {
		err     error
		view    turnkey.View
		problem string
		want    error
	}{
		{fmt.Errorf("%w: offline", turnkey.ErrReaderUnavailable), pending, turnkeyProblemOffline, nil},
		{turnkey.ErrQuota, pending, turnkeyProblemQuota, nil},
		{turnkey.ErrReaderRefused, pending, turnkeyProblemRefused, nil},
		{turnkey.ErrReaderUnavailable, turnkey.View{}, "", ErrTurnkeyUnavailable},
		{fmt.Errorf("%w: %w", turnkey.ErrNotFound, shared.ErrNotFound), turnkey.View{}, "", ErrTurnkeyNotFound},
		{turnkey.ErrInvalidRequest, turnkey.View{}, "", ErrTurnkeyInvalid},
		{errors.New("disk"), turnkey.View{}, "", ErrTurnkeyUnavailable},
	}
	for _, tt := range tests {
		s := NewTurnkeyService(&fakeTurnkey{view: tt.view, err: tt.err}, discard())
		got, err := s.SkipQuestions(t.Context(), "w1")
		if !errors.Is(err, tt.want) || got.Problem != tt.problem || (tt.problem != "" && got.Message == "") {
			t.Errorf("%v: %+v, %v", tt.err, got, err)
		}
	}
	s := NewTurnkeyService(&fakeTurnkey{err: turnkey.ErrNotConfigured}, discard())
	if _, err := s.AskStep(t.Context(), "w1", 0, "?"); !errors.Is(err, ErrTurnkeyNoStepHelp) {
		t.Fatalf("AskStep without an advisor = %v", err)
	}
}
