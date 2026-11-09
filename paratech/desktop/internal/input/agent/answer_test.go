package agent

import (
	"context"
	"fmt"
	"log/slog"
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
	scope    string
}

type fakeAnswerer struct {
	got   answering.Asked
	err   error
	asked []askedQuestion
}

func (f *fakeAnswerer) AskNow(_ context.Context, q string, s shared.SubjectID, scope string, _ answering.DraftSink) (answering.Asked, error) {
	f.asked = append(f.asked, askedQuestion{q, s, scope})
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
	}}, "ag:th1:1")
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
	asker := &fakeAnswerer{got: answering.Asked{Answer: madeAnswer(t)}}
	tl.answerer = asker
	r := tl.run(t.Context(), agentturn.Step{Thread: "th1", Turn: 3, Scope: inPhysics}, answerCall(t, `{"question":"Что такое ряд?"}`))
	if len(r.cards) != 1 || r.cards[0] != (values.AnswerCard{Answer: mustRef(t, values.KindAnswer, "an1")}) {
		t.Fatalf("cards = %+v", r.cards)
	}

	// The answer is reused per call (ADR-0026 addendum 2026-11-08, item 3): «Продолжить» replays the same step with
	// the same call id, so the same scope (a free replay); another call of the request, or another turn, a new one.
	tl.run(t.Context(), agentturn.Step{Thread: "th1", Turn: 3, Scope: inPhysics}, answerCall(t, `{"question":"Что такое ряд?"}`))
	second := agentturn.Call{Call: mustCall(t, "c2", values.ToolAnswerQuestion, `{"question":"Что такое ряд?"}`),
		Class: actionpolicy.ClassR, Paid: true, PlanStep: -1}
	tl.run(t.Context(), agentturn.Step{Thread: "th1", Turn: 3, Scope: inPhysics}, second)
	tl.run(t.Context(), agentturn.Step{Thread: "th1", Turn: 4, Scope: inPhysics}, answerCall(t, `{"question":"Что такое ряд?"}`))
	wantScopes := []string{"ag:th1:3:d0f631ca", "ag:th1:3:d0f631ca", "ag:th1:3:9c0abe51", "ag:th1:4:d0f631ca"}
	if len(asker.asked) != len(wantScopes) || tl.reuse != "" {
		t.Fatalf("asked %+v, tools scope %q", asker.asked, tl.reuse)
	}
	for i, want := range wantScopes {
		if asker.asked[i].scope != want {
			t.Fatalf("scope %d = %q, want %q", i, asker.asked[i].scope, want)
		}
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

// The scope of a call fits the key's bounds whatever the model's call id: a thread id as the app makes it (26 base32
// characters), a large turn, a long or non-ASCII call id.
func TestCallScopeFitsTheKey(t *testing.T) {
	prefix := answerScope(agentturn.Step{Thread: shared.ThreadID(strings.Repeat("a", 26)), Turn: 1_000_000})
	for _, id := range []string{"c1", "toolu_01ABCdef", strings.Repeat("я", 500), "a b\tc"} {
		s := callScope(prefix, id)
		if len(s) > answer.MaxScopeBytes || !strings.HasPrefix(s, prefix+":") || len(s) != len(prefix)+9 {
			t.Fatalf("scope %q (%d bytes)", s, len(s))
		}
		if _, err := answer.NewRequest("Что такое ряд?", []answer.Excerpt{{Text: "Ряд сходится.",
			Source: answer.Source{Recording: "r1", Section: answer.SectionLectureText}}}, s); err != nil {
			t.Fatalf("scope %q refused: %v", s, err)
		}
	}
}

// Beta 2026-11-03 (TASK-366): a refused answer became «материала по этой паре ещё нет». Whatever made answer_question
// fail, the planner sees one fixed error with no cause, so it can only say that an error happened; the cause is in
// desktop.log only, where a model answer refused by the schema reads apart from a provider that did not answer.
func TestAnswerQuestionFailureIsPlain(t *testing.T) {
	const (
		modelAnswer  = "hubclient: answer question: llm provider failed: the model answer breaks the operation schema"
		providerDown = "hubclient: answer question: llm provider failed"
	)
	tests := []struct {
		name string
		err  error
		log  string
	}{
		{"model answer refused by the Hub", fmt.Errorf("%w: %s", answering.ErrReaderRefused, modelAnswer), modelAnswer},
		{"model answer refused by the desktop", fmt.Errorf("%w: %w", answering.ErrReaderRefused, answer.ErrInvalidAnswer),
			answer.ErrInvalidAnswer.Error()},
		{"provider did not answer", fmt.Errorf("%w: %s", answering.ErrNoConnection, providerDown), providerDown},
		{"quota", answering.ErrQuota, answering.ErrQuota.Error()},
		{"local store", answering.ErrUnavailable, answering.ErrUnavailable.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs strings.Builder
			tl := newWorld(t).tools(t)
			tl.log = slog.New(slog.NewTextHandler(&logs, nil))
			tl.answerer = &fakeAnswerer{err: tt.err}
			out := tl.read(t.Context(), values.Scope{}, answerCall(t, `{"question":"Что было на этой паре?"}`))
			if out.Result.ResultJSON != `{"error":"failed"}` || !out.Result.IsError || len(out.Seen) != 0 {
				t.Fatalf("result = %+v", out)
			}
			if !strings.Contains(logs.String(), tt.log) {
				t.Fatalf("desktop.log lacks %q: %s", tt.log, logs.String())
			}
			if strings.Contains(logs.String(), "Что было") {
				t.Fatalf("desktop.log has the question: %s", logs.String())
			}
		})
	}
}
