package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/coursework/work"
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
}

func (f *fakeTurnkey) Work(context.Context, shared.WorkID) (turnkey.View, error) { return f.view, f.err }

func (f *fakeTurnkey) WorkOf(_ context.Context, src work.Source) (turnkey.View, error) {
	f.src = src
	return f.view, f.err
}

func (f *fakeTurnkey) AnswerQuestions(_ context.Context, _ shared.WorkID, r []work.Reply) (turnkey.View, error) {
	f.replies = r
	return f.view, f.err
}

func (f *fakeTurnkey) SkipQuestions(context.Context, shared.WorkID) (turnkey.View, error) { return f.view, f.err }

func (f *fakeTurnkey) EditUnderstanding(_ context.Context, _ shared.WorkID, u work.Understanding) (turnkey.View, error) {
	f.edit = u
	return f.view, f.err
}

func (f *fakeTurnkey) ConfirmRequirements(context.Context, shared.WorkID) (turnkey.View, error) {
	return f.view, f.err
}

func (f *fakeTurnkey) Retry(context.Context, shared.WorkID) (turnkey.View, error) { return f.view, f.err }

func (f *fakeTurnkey) AskStep(context.Context, shared.WorkID, int, string) (string, error) {
	return f.advice, f.err
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
		`"answered":[{"round":1,"question":"Вариант?","answer":"3","default":true}],"version":4,"problem":"","message":""}`
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
