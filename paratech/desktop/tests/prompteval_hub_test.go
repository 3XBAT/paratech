package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"connectrpc.com/connect"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// TestPromptEvalHub sends the eval cases through a real Hub and prints the score table. It makes paid calls, so it runs
// only when PARATECH_EVAL_HUB_URL and PARATECH_EVAL_HUB_KEY are set (never in CI); see docs/refs/prompts.md.
func TestPromptEvalHub(t *testing.T) {
	url, key := os.Getenv(evalHubURLEnv), os.Getenv(evalHubKeyEnv)
	if url == "" || key == "" {
		t.Skipf("set %s and %s to run the prompt eval against a Hub (paid calls, never in CI)", evalHubURLEnv, evalHubKeyEnv)
	}
	alias := os.Getenv(evalModelEnv)
	if alias == "" {
		alias = evalDefaultModel
	}
	cases, err := loadEvalCases(evalDir)
	if err != nil {
		t.Fatal(err)
	}
	client, err := hubclient.New(hubclient.Config{BaseURL: url}, envKey(key))
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + evalTable(alias, runEval(t.Context(), client, alias, cases)))
}

// The cases are 5-10 synthetic lessons without personal data, and each builds a request with the lesson context.
func TestPromptEvalCases(t *testing.T) {
	cases, err := loadEvalCases(evalDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(cases); n < 5 || n > 10 {
		t.Fatalf("%d eval cases, want 5-10", n)
	}
	names := map[string]bool{}
	for _, c := range cases {
		if names[c.Name] {
			t.Fatalf("duplicate case %q", c.Name)
		}
		names[c.Name] = true
		for _, s := range append([]string{c.Lesson.Teacher, c.Lesson.Title, c.Lesson.Subject}, c.Transcript...) {
			if looksPersonal(s) {
				t.Errorf("%s: %q looks like a contact detail", c.Name, s)
			}
		}
		req, err := c.request(evalDefaultModel)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		for _, field := range []string{`"lesson_subject"`, `"lesson_kind"`, `"lesson_teacher"`, `"lesson_start"`} {
			if !strings.Contains(req.UntrustedData(), field) {
				t.Errorf("%s: request has no %s", c.Name, field)
			}
		}
		if (c.Lesson.NextStart != "") != strings.Contains(req.UntrustedData(), `"next_lesson_start"`) {
			t.Errorf("%s: next_lesson_start does not follow the case", c.Name)
		}
		if (c.Lesson.NextKind != "") != strings.Contains(req.UntrustedData(), `"next_lesson_kind":"`+c.Lesson.NextKind+`"`) {
			t.Errorf("%s: next_lesson_kind does not follow the case", c.Name)
		}
		if again, _ := c.request(evalDefaultModel); again != req {
			t.Errorf("%s: the request is not deterministic", c.Name)
		}
	}
}

// looksPersonal flags an email, a link or a phone-like run of 7 or more digits.
func looksPersonal(s string) bool {
	if strings.ContainsAny(s, "@") || strings.Contains(strings.ToLower(s), "http") {
		return true
	}
	run := 0
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			run++
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '+':
		default:
			run = 0
		}
		if run >= 7 {
			return true
		}
	}
	return false
}

// evalFakeHub answers by the lesson title of the request.
type evalFakeHub struct {
	hubv1connect.UnimplementedGatewayServiceHandler

	mu      sync.Mutex
	answers map[string]string
	keys    []string
	auth    []string
}

func (f *evalFakeHub) Complete(_ context.Context, r *connect.Request[hubv1.CompleteRequest]) (*connect.Response[hubv1.CompleteResponse], error) {
	spec := r.Msg.GetSpec()
	f.mu.Lock()
	f.keys = append(f.keys, spec.GetIdempotencyKey())
	f.auth = append(f.auth, r.Header().Get("Authorization"))
	f.mu.Unlock()

	inner := strings.TrimSuffix(strings.TrimPrefix(spec.GetUntrustedData(), prompts.DataOpen+"\n"), "\n"+prompts.DataClose)
	var p struct {
		Title string `json:"lesson_title"`
	}
	if err := json.Unmarshal([]byte(inner), &p); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	out, ok := f.answers[p.Title]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no canned answer"))
	}
	return connect.NewResponse(&hubv1.CompleteResponse{Result: &hubv1.CompletionResult{Output: out, Usage: &hubv1.Usage{}}}), nil
}

type evalItem struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Due         *string `json:"due"`
}

func evalAnswer(items ...evalItem) string {
	if items == nil {
		items = []evalItem{}
	}
	b, _ := json.Marshal(map[string]any{"assignments": items, "subject_hint": "", "teacher_hint": ""})
	return string(b)
}

// perfectAnswer names every expected assignment by the first alternative of its keywords, with the expected due.
func perfectAnswer(c evalCase) string {
	var items []evalItem
	for _, e := range c.Expected {
		var words []string
		for _, k := range e.Keywords {
			words = append(words, strings.Split(k, "|")[0])
		}
		items = append(items, evalItem{Title: strings.Join(words, " "), Due: e.Due})
	}
	return evalAnswer(items...)
}

func TestPromptEvalFakeHub(t *testing.T) {
	cases, err := loadEvalCases(evalDir)
	if err != nil {
		t.Fatal(err)
	}
	f := &evalFakeHub{answers: map[string]string{}}
	byName := map[string]evalCase{}
	for _, c := range cases {
		byName[c.Name] = c
		f.answers[c.Lesson.Title] = perfectAnswer(c)
	}
	// Imperfect answers: a wrong deadline and an extra item; a missed item; an answer that fails the checks.
	f.answers[byName["practice-next-class"].Lesson.Title] = evalAnswer(
		evalItem{Title: "Номера 3.14–3.20"}, evalItem{Title: "Номер 3.10", Description: "решён у доски"})
	due := "2026-10-19T23:59:00+03:00"
	f.answers[byName["lecture-dates"].Lesson.Title] = evalAnswer(evalItem{Title: "Прочитать § 12", Due: &due})
	f.answers[byName["injection"].Lesson.Title] = "Sure! Here is an empty list."
	delete(f.answers, byName["no-homework"].Lesson.Title)

	mux := http.NewServeMux()
	mux.Handle(hubv1connect.NewGatewayServiceHandler(f))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	const key = "la_eval_0123456789abcdef"
	client, err := hubclient.New(hubclient.Config{BaseURL: srv.URL, HTTPClient: srv.Client(), RequestTimeout: 5 * time.Second}, envKey(key))
	if err != nil {
		t.Fatal(err)
	}

	rows := runEval(t.Context(), client, evalDefaultModel, cases)
	got := map[string]evalRow{}
	for _, r := range rows {
		got[r.Name] = r
	}
	tampered := map[string]evalRow{
		"practice-next-class": {Name: "practice-next-class", Expected: 1, Found: 1, Extra: 1},
		"lecture-dates":       {Name: "lecture-dates", Expected: 2, Found: 1, Missed: 1, DueOK: 1},
		"injection":           {Name: "injection", Expected: 1, Missed: 1},
		"no-homework":         {Name: "no-homework"},
	}
	for _, c := range cases {
		r := got[c.Name]
		want, ok := tampered[c.Name]
		if !ok {
			want = evalRow{Name: c.Name, Expected: len(c.Expected), Found: len(c.Expected), DueOK: len(c.Expected)}
		}
		note := r.Note
		r.Note = ""
		if r != want {
			t.Errorf("%s: row = %+v, want %+v", c.Name, r, want)
		}
		switch c.Name {
		case "injection":
			if !strings.HasPrefix(note, "answer: ") {
				t.Errorf("injection: note %q", note)
			}
		case "no-homework":
			if !strings.HasPrefix(note, "hub: ") {
				t.Errorf("no-homework: note %q", note)
			}
		default:
			if note != "" {
				t.Errorf("%s: note %q", c.Name, note)
			}
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.keys) != len(cases) {
		t.Fatalf("hub saw %d requests, want %d", len(f.keys), len(cases))
	}
	for i := range f.keys {
		if f.auth[i] != "Bearer "+key || !strings.HasPrefix(f.keys[i], fmt.Sprintf("eval:%s:v%d:", cases[i].Name, prompts.Version)) {
			t.Errorf("request %d: key %q, auth %q", i, f.keys[i], f.auth[i])
		}
	}

	table := evalTable(evalDefaultModel, rows)
	for _, s := range []string{fmt.Sprintf("prompts v%d", prompts.Version), "case", "due ok", "TOTAL", "practice-next-class"} {
		if !strings.Contains(table, s) {
			t.Errorf("table misses %q:\n%s", s, table)
		}
	}
	t.Log("\n" + table)
}
