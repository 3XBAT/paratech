package agentstep_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/domain/llm/services/seal"
	"paratech/hub/internal/domain/llm/services/toolcatalog"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/testenv"
)

// longSentence is one sentence of a long explanation; longText is 5300 characters of them (TASK-223).
const longSentence = "Преобразование Фурье раскладывает сигнал на частоты. "

var longText = strings.Repeat(longSentence, 100)

// newVersionFixture is newFixture serving the panel catalogs 6 and 7 side by side.
func newVersionFixture(t *testing.T, steps ...testenv.AgentProviderStep) fixture {
	t.Helper()
	var versions []agentstep.Version
	for _, v := range []uint32{6, 7} {
		system, catalog, err := testenv.Panel(v)
		if err != nil {
			t.Fatal(err)
		}
		versions = append(versions, agentstep.Version{System: system, Catalog: catalog})
	}
	sealer, err := testenv.Sealer(1)
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{
		store: testenv.NewQuotaStore(), turns: testenv.NewAgentTurns(), provider: testenv.NewFakeAgentProvider(steps...),
		limiter: &testenv.FakeLimiter{}, clock: testenv.NewFakeClock(t0), sealer: sealer, logs: &syncBuffer{},
		system: versions[1].System, catalog: versions[1].Catalog,
	}
	f.svc, err = agentstep.New(agentstep.Deps{
		Accounts: f.store, Turns: f.turns, Provider: f.provider, Limiter: f.limiter, Clock: f.clock, Random: testenv.Random{}, Models: models(t),
		Sealer: sealer, Log: slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}, agentstep.Config{Unit: "tokens", Limit: 1_000_000, ReservationTTL: ttl, KeyRetention: retention, CallTimeout: callTimeout,
		Profiles: []agentstep.Profile{{Name: values.ProfilePanel, Alias: "planner", Versions: versions}}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func commandV(key string, version uint32, turns ...agentstep.Turn) agentstep.Command {
	cmd := command(alice, key, turns...)
	cmd.CatalogVersion = version
	return cmd
}

func bindingV(version uint32) seal.Binding {
	b := binding(alice)
	b.CatalogVersion = version
	return b
}

// TestLongTextAnswerIsCutUnderCatalog7: a text-only answer over 4000 characters becomes a reply cut at a sentence with
// " …", charged once like any answer and replayed as it is; exactly 4000 characters and angle brackets pass unchanged.
func TestLongTextAnswerIsCutUnderCatalog7(t *testing.T) {
	f := newVersionFixture(t, textAnswer(t, values.StopEnd, longText))
	res := f.step(t, commandV("ag:1", 7))

	got := shownReply(t, res)
	if n := utf8.RuneCountInString(got); n > toolcatalog.MaxReplyTextRunes || !strings.HasSuffix(got, ". …") ||
		!strings.HasPrefix(longText, strings.TrimSuffix(got, " …")) {
		t.Fatalf("reply: %d characters, ends %q", n, got[len(got)-24:])
	}
	if err := f.sealer.Verify(bindingV(7), modelCalls(res, t), res.Seal); err != nil {
		t.Fatalf("the seal does not verify: %v", err)
	}
	if used, held := f.store.Totals(alice); used != 982 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "outcome=completed") || !strings.Contains(logs, "text_reply=true") || strings.Contains(logs, "Фурье") {
		t.Fatalf("log: %s", logs)
	}

	again := f.step(t, commandV("ag:1", 7))
	if !again.Replayed || shownReply(t, again) != got || len(f.provider.Calls()) != 1 {
		t.Fatalf("retry = %+v, provider calls %d", again, len(f.provider.Calls()))
	}

	for name, text := range map[string]string{
		"exact":  strings.Repeat("я", toolcatalog.MaxReplyTextRunes),
		"angles": "Если a < b, то b > a; <script> остаётся текстом.",
	} {
		f := newVersionFixture(t, textAnswer(t, values.StopEnd, text))
		if got := shownReply(t, f.step(t, commandV("ag:1", 7))); got != text {
			t.Errorf("%s: reply text changed: %q", name, got)
		}
	}
}

// TestLongTextAnswerUnderCatalog6IsTheOldFailure: catalog 6 keeps its 300-character reply and its refusal of angle
// brackets: the charged "no tool use" failure, never a cut.
func TestLongTextAnswerUnderCatalog6IsTheOldFailure(t *testing.T) {
	for name, text := range map[string]string{"long": longText, "301": strings.Repeat("я", 301), "angles": "a < b"} {
		f := newVersionFixture(t, textAnswer(t, values.StopEnd, text))
		_, err := f.svc.Step(t.Context(), commandV("ag:1", 6))
		var failed *agentstep.ProviderFailedError
		if !errors.As(err, &failed) || !strings.Contains(err.Error(), "no tool use") {
			t.Errorf("%s: err = %v, want the old failure", name, err)
		}
		if used, _ := f.store.Totals(alice); used != 982 || !strings.Contains(f.logs.String(), "text_reply=false") {
			t.Errorf("%s: used %d, log %s", name, used, f.logs.String())
		}
	}
}

// TestLongReplyCallIsCutUnderCatalog7: a returned reply call over the limit is cut before the schema check, sealed and
// stored cut, logged as reply_cut; its echo finds the kept turn, so the next step runs. Under catalog 6 the same call
// is the charged invalid-calls failure.
func TestLongReplyCallIsCutUnderCatalog7(t *testing.T) {
	over := strings.Repeat("Ответ длинный. ", 300) // 4500 characters, within the 8 KiB of the wire
	b, err := json.Marshal(map[string]any{"text": over, "assignments": []string{"asg-1"}})
	if err != nil {
		t.Fatal(err)
	}
	long := func() testenv.AgentProviderStep {
		return answer(t, 1200, 80, toolCall(t, "toolu_01", "find_lessons", `{"subject":"subj-1"}`), toolCall(t, "toolu_02", "reply", string(b)))
	}

	f := newVersionFixture(t, long(), twoCalls(t))
	res := f.step(t, commandV("ag:1", 7))
	if len(res.Calls) != 2 || res.Calls[0].Arguments != `{"subject":"subj-1"}` || res.Calls[1].ID != "toolu_02" {
		t.Fatalf("calls = %+v", res.Calls)
	}
	var args struct {
		Text        string   `json:"text"`
		Assignments []string `json:"assignments"`
	}
	if err := json.Unmarshal([]byte(res.Calls[1].Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(args.Text) > toolcatalog.MaxReplyTextRunes || !strings.HasSuffix(args.Text, "длинный. …") ||
		len(args.Assignments) != 1 || args.Assignments[0] != "asg-1" {
		t.Fatalf("reply arguments = %q", res.Calls[1].Arguments)
	}
	if err := f.sealer.Verify(bindingV(7), modelCalls(res, t), res.Seal); err != nil {
		t.Fatalf("the seal does not verify: %v", err)
	}
	if logs := f.logs.String(); !strings.Contains(logs, "reply_cut=true") || !strings.Contains(logs, "outcome=completed") || strings.Contains(logs, "длинный") {
		t.Fatalf("log: %s", logs)
	}
	if again := f.step(t, commandV("ag:1", 7)); !again.Replayed || again.Calls[1].Arguments != res.Calls[1].Arguments {
		t.Fatalf("replay = %+v", again)
	}

	if _, err := f.svc.Step(t.Context(), commandV("ag:2", 7, echo(res)...)); err != nil {
		t.Fatalf("the echo of the cut calls: %v", err)
	}

	old := newVersionFixture(t, long())
	_, err = old.svc.Step(t.Context(), commandV("ag:1", 6))
	if err == nil || !strings.Contains(err.Error(), "invalid tool calls") || strings.Contains(old.logs.String(), "reply_cut") {
		t.Fatalf("v6: err = %v, log %s", err, old.logs.String())
	}
	if used, _ := old.store.Totals(alice); used != 1280 {
		t.Fatalf("v6: used %d, want the billed answer charged", used)
	}
}
