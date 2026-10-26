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
	cvalues "paratech/desktop/internal/domain/coursework/values"
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
	reused  bool // StartWriting found its job (Created false)
}

func (f *fakeTurnkey) Triage(_ context.Context, src work.Source) (turnkey.Started, error) {
	f.src = src
	return turnkey.Started{View: f.view, Created: true}, f.err
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
	return turnkey.WritingStarted{View: f.view, Job: "j1", Created: !f.reused}, f.err
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
		`{"number":1,"kind":"","part":"","title":"Введение","words":200,"note":"","written":true,"paragraphs":["Абзац."],` +
		`"sources":[{"text":"Методичка.docx","materialId":"m1","verify":false},{"text":"Иванов И. И.","materialId":"","verify":true}],` +
		`"bullets":[],"columns":[]},` +
		`{"number":2,"kind":"","part":"","title":"Итоги","words":150,"note":"","written":false,"paragraphs":[],"sources":[],` +
		`"bullets":[],"columns":[]}],` +
		`"done":1,"total":2,"subjectId":"s1","materialId":"","result":"","job":"","active":false,"jobState":"",` +
		`"presentation":false,"template":"","questions":[],"talkMaterialId":"","talkResult":"",` +
		`"lab":false,"manual":"","placeholders":[]}`
	if string(b) != want {
		t.Fatalf("JSON =\n%s\nwant\n%s", b, want)
	}
}

// A presentation and a lab report bring their own data to the card; a plain work has none (false, "" and []).
func TestTurnkeyWritingKinds(t *testing.T) {
	talk, err := shared.NewArtifactRef(shared.ArtifactExports, "homework/w1-talk.docx")
	if err != nil {
		t.Fatal(err)
	}

	deck := writingWork()
	deck.Status = work.StatusDone
	deck.Writing.Step, deck.Writing.Material = work.StepNone, "m-pptx"
	deck.Writing.Presentation, deck.Writing.Template = true, cvalues.PresentationGraphite
	deck.Writing.Questions = []work.TalkQuestion{{Question: "Почему так?", Answer: "Потому что."}}
	deck.Writing.Talk, deck.Writing.TalkResult = "m-talk", talk
	deck.Writing.Sections = []turnkey.SectionView{
		{Number: 1, Kind: cvalues.SlideBullets, Title: "Причины", Written: true, Paragraphs: []string{"Заметки."},
			Bullets: []string{"Первое", "Второе"}},
		{Number: 2, Kind: cvalues.SlideTwoColumns, Title: "Сравнение", Written: true,
			Columns: []work.Column{{Heading: "До", Bullets: []string{"а"}}, {Heading: "После"}}},
		{Number: 3, Kind: cvalues.SlideConclusion, Title: "Итоги"},
	}

	lab := writingWork()
	lab.Writing.Lab, lab.Writing.Manual = true, work.ManualNotRead
	lab.Writing.Placeholders = []work.Placeholder{{Section: 2, Title: "Результаты", Prompt: "▢ Вставьте таблицу измерений"}}
	lab.Writing.Sections = []turnkey.SectionView{
		{Number: 1, Part: work.LabGoal, Title: "Цель", Written: true, Paragraphs: []string{"Измерить."}},
		{Number: 2, Part: work.LabResults, Title: "Результаты", Written: true, Paragraphs: []string{"▢ Вставьте таблицу измерений"}},
	}

	tests := map[string]struct {
		view turnkey.View
		want string
	}{
		"presentation": {deck, `"materialId":"m-pptx","result":"","job":"","active":false,"jobState":"",` +
			`"presentation":true,"template":"graphite","questions":[{"question":"Почему так?","answer":"Потому что."}],` +
			`"talkMaterialId":"m-talk","talkResult":"exports/homework/w1-talk.docx","lab":false,"manual":"","placeholders":[]}`},
		"lab": {lab, `"presentation":false,"template":"","questions":[],"talkMaterialId":"","talkResult":"",` +
			`"lab":true,"manual":"not_read","placeholders":[{"section":2,"title":"Результаты","prompt":"▢ Вставьте таблицу измерений"}]}`},
		"plain": {writingWork(), `"presentation":false,"template":"","questions":[],"talkMaterialId":"","talkResult":"",` +
			`"lab":false,"manual":"","placeholders":[]}`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := NewTurnkeyService(&fakeTurnkey{view: tt.view}, discard()).Work(t.Context(), "w1")
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(got.Writing)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(string(b), tt.want) {
				t.Fatalf("JSON =\n%s\nwant the end\n%s", b, tt.want)
			}
		})
	}

	sections := func(v turnkey.View) string {
		got, err := NewTurnkeyService(&fakeTurnkey{view: v}, discard()).Work(t.Context(), "w1")
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(got.Writing.Sections)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	wantDeck := `[{"number":1,"kind":"bullets","part":"","title":"Причины","words":0,"note":"","written":true,` +
		`"paragraphs":["Заметки."],"sources":[],"bullets":["Первое","Второе"],"columns":[]},` +
		`{"number":2,"kind":"two_columns","part":"","title":"Сравнение","words":0,"note":"","written":true,` +
		`"paragraphs":[],"sources":[],"bullets":[],"columns":[{"heading":"До","bullets":["а"]},{"heading":"После","bullets":[]}]},` +
		`{"number":3,"kind":"conclusion","part":"","title":"Итоги","words":0,"note":"","written":false,` +
		`"paragraphs":[],"sources":[],"bullets":[],"columns":[]}]`
	if got := sections(deck); got != wantDeck {
		t.Errorf("slides =\n%s\nwant\n%s", got, wantDeck)
	}
	wantLab := `[{"number":1,"kind":"","part":"goal","title":"Цель","words":0,"note":"","written":true,` +
		`"paragraphs":["Измерить."],"sources":[],"bullets":[],"columns":[]},` +
		`{"number":2,"kind":"","part":"results","title":"Результаты","words":0,"note":"","written":true,` +
		`"paragraphs":["▢ Вставьте таблицу измерений"],"sources":[],"bullets":[],"columns":[]}]`
	if got := sections(lab); got != wantLab {
		t.Errorf("lab sections =\n%s\nwant\n%s", got, wantLab)
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

// A job StartWriting found is the job of the card only while it is active: a finished one is no job.
func TestTurnkeyStartedReusedJob(t *testing.T) {
	for _, tt := range []struct {
		name   string
		active bool
		want   string
	}{
		{"finished job", false, ""},
		{"active job", true, "j1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := writingWork()
			if tt.active {
				v.Writing.Active, v.Writing.Job, v.Writing.JobState = true, "j1", job.StatusRunning
			}
			s := NewTurnkeyService(&fakeTurnkey{view: v, reused: true}, discard())
			got, err := s.StartWriting(t.Context(), "w1")
			if err != nil || got.Writing.Job != tt.want || got.Writing.Active != tt.active {
				t.Fatalf("StartWriting = %+v, %v; want job %q", got.Writing, err, tt.want)
			}
		})
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
		ID: "w1", Source: work.Source{Assignment: "a1"}, Sendable: true, Status: work.StatusHelper, Round: 1, HasAnswer: true,
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
	want := `{"id":"w1","assignmentId":"a1","materialId":"","topic":"","subjectId":"","sendable":true,"status":"helper",` +
		`"round":1,"questionRound":0,"questions":[],` +
		`"hasCard":true,"card":{"workType":"lab_report","volume":"","fileFormat":"dwg","formatting":"","variant":"",` +
		`"deadline":"","sources":""},"edited":["file_format"],"feasibility":"impossible",` +
		`"feasibilityReason":"Чертёж в программе вуза","partial":false,"assumptions":[],` +
		`"helper":{"steps":["Открыть программу"],"submit":"Файл .dwg","pitfalls":[],"readyParts":["Записка"],"placeholders":[]},` +
		`"answered":[{"round":1,"question":"Вариант?","answer":"3","default":true}],"writing":null,"version":4,"problem":"","message":""}`
	if string(b) != want {
		t.Fatalf("JSON =\n%s\nwant\n%s", b, want)
	}
}

// TASK-192: a work on a topic carries its text and subject, and is not sendable; an uploaded file is not either.
func TestTurnkeyWorkOfATopic(t *testing.T) {
	topic, err := work.NewTopic("ui:press-1", "История  Древней Руси", "s1")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name             string
		src              work.Source
		topic, subjectID string
	}{
		{"topic with a subject", work.Source{Topic: topic}, "История Древней Руси", "s1"},
		{"uploaded file", work.Source{Material: "m1"}, "", ""},
	}
	for _, tt := range tests {
		v := helperView()
		v.Source, v.Sendable = tt.src, false
		got, err := NewTurnkeyService(&fakeTurnkey{view: v}, discard()).Work(t.Context(), "w1")
		if err != nil || got.Topic != tt.topic || got.SubjectID != tt.subjectID || got.Sendable || got.AssignmentID != "" {
			t.Errorf("%s: %+v, %v", tt.name, got, err)
		}
	}
}

// TASK-192: AskStep words no connection, the quota and a refused answer apart from the generic error.
func TestTurnkeyAskStepErrors(t *testing.T) {
	tests := []struct {
		err  error
		want error
	}{
		{fmt.Errorf("%w: %w", turnkey.ErrReaderUnavailable, shared.ErrNetworkUnavailable), ErrTurnkeyStepOffline},
		{fmt.Errorf("%w: %w", turnkey.ErrQuota, shared.ErrQuotaExceeded), ErrTurnkeyStepQuota},
		{fmt.Errorf("%w: %w", turnkey.ErrReaderRefused, work.ErrInvalidAnswer), ErrTurnkeyStepRefused},
		{turnkey.ErrNotConfigured, ErrTurnkeyNoStepHelp},
		{turnkey.ErrInvalidRequest, ErrTurnkeyInvalid},
		{fmt.Errorf("%w: %w", turnkey.ErrNotFound, shared.ErrNotFound), ErrTurnkeyNotFound},
		{errors.New("disk"), ErrTurnkeyUnavailable},
	}
	texts := map[string]bool{}
	for _, tt := range tests {
		_, err := NewTurnkeyService(&fakeTurnkey{err: tt.err}, discard()).AskStep(t.Context(), "w1", 0, "Как начать?")
		if !errors.Is(err, tt.want) {
			t.Errorf("%v: AskStep = %v, want %v", tt.err, err, tt.want)
		}
		if err != nil && texts[err.Error()] {
			t.Errorf("%v: the text %q is not its own", tt.err, err)
		}
		if err != nil {
			texts[err.Error()] = true
		}
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
