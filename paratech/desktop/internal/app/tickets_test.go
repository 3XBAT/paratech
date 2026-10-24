package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/cases/tickets"
	"paratech/desktop/internal/domain/coursework/answer"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/shared"
)

var _ tickets.Answerer = ticketAnswers{}

type ticketLessons struct{}

func (ticketLessons) Texts(context.Context, shared.SubjectID) ([]kb.LessonText, error) {
	return []kb.LessonText{{
		Lesson: kb.Lesson{Recording: "r1", StartedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC), Title: "Ряды"},
		Document: kb.Document{Recording: "r1", Blocks: []kb.Block{{Heading: "Сходимость", Paragraphs: []string{
			"Ряд сходится абсолютно, если сходится ряд модулей.",
		}}}},
	}}, nil
}

type ticketReader struct{ err error }

func (r ticketReader) AnswerQuestion(context.Context, answer.Request) (string, error) { return "", r.err }

type ticketAnswerStore struct{}

func (ticketAnswerStore) Get(context.Context, shared.AnswerID) (*answer.Answer, error) {
	return nil, shared.ErrNotFound
}

func (ticketAnswerStore) FindByKey(context.Context, string) (*answer.Answer, error) {
	return nil, shared.ErrNotFound
}

func (ticketAnswerStore) Insert(context.Context, *answer.Answer) error { return nil }

type ticketClock struct{}

func (ticketClock) Now() time.Time { return time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC) }

type ticketIDs struct{}

func (ticketIDs) NewAnswerID() shared.AnswerID { return "an1" }

// The answers of a ticket run give the errors the tickets port names: a refusal is recorded, no connection or the
// quota lets the job wait, a missing answer is not found.
func TestTicketAnswersMapsErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"refused", fmt.Errorf("hub: %w", answer.ErrInvalidAnswer), answer.ErrInvalidAnswer},
		{"quota", fmt.Errorf("hub: %w", shared.ErrQuotaExceeded), shared.ErrQuotaExceeded},
		{"not configured", shared.ErrServiceNotConfigured, shared.ErrNetworkUnavailable},
		{"no network", fmt.Errorf("hub: %w", shared.ErrNetworkUnavailable), shared.ErrNetworkUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := answering.New(answering.Deps{
				Knowledge: ticketLessons{}, Reader: ticketReader{err: tt.err}, Answers: ticketAnswerStore{},
				Clock: ticketClock{}, IDs: ticketIDs{},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (ticketAnswers{answers: svc}).Ask(t.Context(), "Когда ряд сходится абсолютно?", "sub1"); !errors.Is(err, tt.want) {
				t.Fatalf("Ask = %v, want %v", err, tt.want)
			}
		})
	}

	svc, err := answering.New(answering.Deps{
		Knowledge: ticketLessons{}, Reader: ticketReader{}, Answers: ticketAnswerStore{}, Clock: ticketClock{}, IDs: ticketIDs{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (ticketAnswers{answers: svc}).Answer(t.Context(), "an1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Answer = %v", err)
	}
	if a, err := (ticketAnswers{answers: svc}).Ask(t.Context(), "Что такое ротор векторного поля?", "sub1"); err != nil || a != nil {
		t.Fatalf("no passage = %v, %v", a, err)
	}
	if err := (runnerCanceller{c: &core{}}).Cancel(t.Context(), "j1"); err == nil {
		t.Fatal("a cancel without a runner must fail")
	}
}
