package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/shared"
)

type askedQuestion struct {
	question string
	subject  shared.SubjectID
}

type fakeAnswerer struct {
	got   answering.Asked
	err   error
	asked []askedQuestion
}

func (f *fakeAnswerer) Ask(_ context.Context, q string, s shared.SubjectID) (answering.Asked, error) {
	f.asked = append(f.asked, askedQuestion{q, s})
	return f.got, f.err
}

// claimText and quote are in the answer only; the planner must never see them.
const (
	claimText = "Ряд сходится, если сходится ряд модулей."
	quote     = "если сходится ряд модулей"
)

func madeAnswer(t *testing.T) *answer.Answer {
	t.Helper()
	req, err := answer.NewRequest("Что такое ряд?", []answer.Excerpt{{
		Text:   "Абсолютная сходимость: ряд сходится, если сходится ряд модулей. " + injection,
		Source: answer.Source{Recording: "r1", Section: answer.SectionLectureText, StartMs: 60_000},
	}})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := req.Parse(`{"claims":[{"text":"` + claimText + `","cites":[{"chunk_id":"f1","quote":"` + quote + `"}]},` +
		`{"text":"Выдумка.","cites":[{"chunk_id":"f1","quote":"такого в лекции нет вовсе"}]}],"missing":["Признак Даламбера"]}`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := answer.New("an1", "s1", req, rep, time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func answerCall(t *testing.T, args string) agentturn.Call {
	t.Helper()
	return agentturn.Call{Call: mustCall(t, "c1", values.ToolAnswerQuestion, args), Class: actionpolicy.ClassR,
		Paid: true, PlanStep: -1}
}

func TestAnswerQuestion(t *testing.T) {
	inPhysics, err := values.NewScope(mustRef(t, values.KindSubject, "s1"), "", values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		args    string
		scope   values.Scope
		got     answering.Asked
		err     error
		want    string
		subject shared.SubjectID
	}{
		{"answer", `{"question":"Что такое ряд?"}`, inPhysics, answering.Asked{Answer: madeAnswer(t)}, nil,
			`{"answer":"an1","claims":2,"fragments":1,"missing":1,"sources":1,"supported":1,"unsupported":1}`, "s1"},
		{"whole base", `{"question":"Что такое ряд?","scope":"all"}`, inPhysics, answering.Asked{Answer: madeAnswer(t)}, nil,
			`{"answer":"an1","claims":2,"fragments":1,"missing":1,"sources":1,"supported":1,"unsupported":1}`, ""},
		{"no fragments", `{"question":"Что такое ротор?"}`, values.Scope{}, answering.Asked{}, nil, `{"fragments":0}`, ""},
		{"invalid question", `{"question":"Что такое ряд?"}`, values.Scope{}, answering.Asked{}, answering.ErrInvalidQuestion,
			`{"error":"invalid_arguments"}`, ""},
		{"no connection", `{"question":"Что такое ряд?"}`, values.Scope{}, answering.Asked{}, answering.ErrNoConnection,
			`{"error":"failed"}`, ""},
		{"quota", `{"question":"Что такое ряд?"}`, values.Scope{}, answering.Asked{}, answering.ErrQuota, `{"error":"failed"}`, ""},
		{"refused answer", `{"question":"Что такое ряд?"}`, values.Scope{}, answering.Asked{},
			fmt.Errorf("%w: %s", answering.ErrReaderRefused, injection), `{"error":"failed"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tl := newWorld(t).tools(t)
			ans := &fakeAnswerer{got: tt.got, err: tt.err}
			tl.answerer = ans
			out := tl.read(t.Context(), tt.scope, answerCall(t, tt.args))
			if out.Result.ResultJSON != tt.want {
				t.Fatalf("result = %s, want %s", out.Result.ResultJSON, tt.want)
			}
			if len(ans.asked) != 1 || ans.asked[0].subject != tt.subject || !strings.HasPrefix(ans.asked[0].question, "Что такое ") {
				t.Fatalf("asked %+v", ans.asked)
			}
			// Only the answer ID and counts reach the planner: no claim, quote, lesson text or error text.
			for _, s := range []string{claimText, quote, "модулей", "Даламбер", "attacker", "Игнорируй"} {
				if strings.Contains(out.Result.ResultJSON, s) {
					t.Fatalf("the planner saw %q: %s", s, out.Result.ResultJSON)
				}
			}
			wantSeen := 0
			if tt.got.Answer != nil {
				wantSeen = 1
			}
			if len(out.Seen) != wantSeen || (wantSeen == 1 && out.Seen[0] != mustRef(t, values.KindAnswer, "an1")) {
				t.Fatalf("seen = %v", out.Seen)
			}
		})
	}

	// The Driver shows an answer as its card (ID only); no answer or a refusal shows none.
	tl := newWorld(t).tools(t)
	tl.answerer = &fakeAnswerer{got: answering.Asked{Answer: madeAnswer(t)}}
	r := tl.run(t.Context(), agentturn.Step{Scope: inPhysics}, answerCall(t, `{"question":"Что такое ряд?"}`))
	if len(r.cards) != 1 || r.cards[0] != (values.AnswerCard{Answer: mustRef(t, values.KindAnswer, "an1")}) {
		t.Fatalf("cards = %+v", r.cards)
	}
	for _, ans := range []*fakeAnswerer{{}, {err: answering.ErrNoConnection}} {
		tl.answerer = ans
		if r := tl.run(t.Context(), agentturn.Step{}, answerCall(t, `{"question":"Что такое ряд?"}`)); len(r.cards) != 0 {
			t.Fatalf("cards without an answer = %+v", r.cards)
		}
	}

	// Without an answerer the paid reader stays unavailable and nothing is asked.
	if out := newWorld(t).tools(t).read(t.Context(), values.Scope{}, answerCall(t, `{"question":"Что такое ряд?"}`)); out.Result.ResultJSON != `{"error":"unavailable"}` {
		t.Fatalf("no answerer: %s", out.Result.ResultJSON)
	}
}
