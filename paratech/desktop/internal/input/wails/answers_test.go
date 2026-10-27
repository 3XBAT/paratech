package wails

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/shared"
)

type fakeAnswers struct {
	a       *answer.Answer
	removed []bool
	err     error
	asks    []shared.AnswerID
}

func (f *fakeAnswers) Card(_ context.Context, id shared.AnswerID) (answering.Card, error) {
	f.asks = append(f.asks, id)
	if f.err != nil {
		return answering.Card{}, f.err
	}
	return answering.Card{Answer: f.a, Removed: f.removed}, nil
}

func cardAnswer(t *testing.T) *answer.Answer {
	t.Helper()
	at := time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC)
	req, err := answer.NewRequest("Что такое ряд?", []answer.Excerpt{
		{Text: "Ряд сходится, если сходится ряд модулей. <b>жирный</b>",
			Source: answer.Source{Recording: "r1", Section: answer.SectionLectureText, Index: 3, StartMs: 725_000, Title: "Матанализ", StartedAt: at}},
		{Text: "Ключевые понятия: ряд, частичная сумма.", Source: answer.Source{Recording: "r2", Section: answer.SectionSummary, Index: 1}},
	}, "ag:th1:1")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := req.Parse(`{"claims":[` +
		`{"text":"Абсолютно сходящийся ряд сходится.","cites":[{"chunk_id":"f1","quote":"если сходится ряд модулей"},{"chunk_id":"f2","quote":"ряд, частичная сумма"}]},` +
		`{"text":"Ряды придумал Коши.","cites":[{"chunk_id":"f1","quote":"Коши придумал ряды вовсе"}]}` +
		`],"missing":["Признак Даламбера"]}`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := answer.New("an1", "s1", req, rep, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// The card gets the question, the claims (supported or not), the quotes and the places they open; text only.
func TestAnswerViewJSON(t *testing.T) {
	f := &fakeAnswers{a: cardAnswer(t)}
	got, err := NewAnswerService(f, discard()).Answer(t.Context(), "an1")
	if err != nil || len(f.asks) != 1 || f.asks[0] != "an1" {
		t.Fatalf("Answer = %v, asks %v", err, f.asks)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"an1","question":"Что такое ряд?","subjectId":"s1","createdAt":"2026-10-20T08:00:00Z","claims":[` +
		`{"text":"Абсолютно сходящийся ряд сходится.","fromLessons":true,"cites":[{"quote":"если сходится ряд модулей","source":0},{"quote":"ряд, частичная сумма","source":1}]},` +
		`{"text":"Ряды придумал Коши.","fromLessons":false,"cites":[]}],"missing":["Признак Даламбера"],"sources":[` +
		`{"recordingId":"r1","section":"lectureText","index":3,"startMs":725000,"time":"12:05","title":"Матанализ","startedAt":"2026-10-20T07:00:00Z",` + noNote + `},` +
		`{"recordingId":"r2","section":"summary","index":1,"startMs":0,"time":"","title":"","startedAt":"",` + noNote + `}]}`
	if string(b) != want {
		t.Fatalf("JSON =\n%s\nwant\n%s", b, want)
	}
}

// noNote is how a source of a recording shows the note fields.
const noNote = `"noteId":"","materialId":"","locatorKind":"","locatorN":0,"label":"","removed":false`

// A source of the student's note maps to the card with its note, file, place and label, no recording and no time.
func TestAnswerViewOwnNote(t *testing.T) {
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	req, err := answer.NewRequest("Что такое ряд?", []answer.Excerpt{
		{Text: "Ряды\nРяд сходится, если сходится ряд модулей.", Source: answer.Source{Section: answer.SectionOwnNote, Index: 2,
			Title: "Матанализ", StartedAt: at, Note: "n1", Material: "m1",
			Locator: answer.Locator{Kind: answer.LocatorSection, N: 4}, Heading: "Ряды"}},
	}, "ag:th1:1")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := req.Parse(`{"claims":[{"text":"Ряд сходится абсолютно.","cites":[{"chunk_id":"f1","quote":"если сходится ряд модулей"}]}],"missing":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := answer.New("an1", "s1", req, rep, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewAnswerService(&fakeAnswers{a: a}, discard()).Answer(t.Context(), "an1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got.Sources)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"recordingId":"","section":"ownNote","index":2,"startMs":0,"time":"","title":"Матанализ",` +
		`"startedAt":"2026-10-20T00:00:00Z","noteId":"n1","materialId":"m1","locatorKind":"section","locatorN":4,` +
		`"label":"ваш конспект · раздел „Ряды“","removed":false}]`
	if string(b) != want || !got.Claims[0].FromLessons {
		t.Fatalf("sources =\n%s\nwant\n%s", b, want)
	}

	// The note removed since: the source says so, the claim and its quote stay.
	removed, err := NewAnswerService(&fakeAnswers{a: a, removed: []bool{true}}, discard()).Answer(t.Context(), "an1")
	if err != nil {
		t.Fatal(err)
	}
	if s := removed.Sources[0]; !s.Removed || s.Label != "источник удалён" || s.NoteID != "n1" ||
		len(removed.Claims[0].Cites) != 1 || !removed.Claims[0].FromLessons {
		t.Fatalf("removed = %+v", removed)
	}
}

func TestAnswerErrors(t *testing.T) {
	tests := []struct {
		err  error
		want error
	}{
		{answering.ErrNotFound, ErrAnswerNotFound},
		{errors.Join(answering.ErrUnavailable, errors.New("disk")), ErrAnswerUnavailable},
	}
	for _, tt := range tests {
		if _, err := NewAnswerService(&fakeAnswers{err: tt.err}, discard()).Answer(t.Context(), "an1"); !errors.Is(err, tt.want) {
			t.Errorf("Answer(%v) = %v, want %v", tt.err, err, tt.want)
		}
	}
}
