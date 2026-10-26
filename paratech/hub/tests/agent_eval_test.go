package tests

// Agent checks, level 1, Hub side (TASK-135, design §3.3, ADR-0023): the recorded answers and the injection fixtures
// of shared/testdata/agent through the real router, key check, AgentStep handler and use case, seal service and tool
// catalog. Only the provider, the quota store and the kept-turn store are fakes, so no Postgres is needed.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/cases/keyauth"
	"paratech/hub/internal/cases/keys"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/services/seal"
	"paratech/hub/internal/domain/llm/services/toolcatalog"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/internal/input/connect"
	"paratech/hub/pkg/prompts"
	"paratech/hub/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

const evalThread = "th-eval"

// evalHub is the Hub's public listener with AgentStep wired to the real use case.
type evalHub struct {
	provider *testenv.FakeAgentProvider
	store    *testenv.QuotaStore
	sealer   *seal.Sealer
	catalog  *toolcatalog.Catalog
	system   string
	client   hubv1connect.GatewayServiceClient
	keys     map[shared.APIKeyID]string // issued key ID -> bearer key
	owner    shared.APIKeyID
	version  uint32 // the panel catalog version the hub serves
}

// noCompleter stands for Complete, which these checks do not use.
type noCompleter struct{}

func (noCompleter) Complete(_ context.Context, _ gateway.Command) (gateway.Result, error) {
	return gateway.Result{}, errors.New("not used")
}

type readyProbe struct{}

func (readyProbe) Ping(context.Context) error { return nil }

func newEvalHub(t *testing.T) *evalHub {
	t.Helper()
	return newEvalHubOf(t, 1, 512)
}

// newEvalHubOf serves only panel catalog version with the planner alias capped at plannerCap output tokens.
func newEvalHubOf(t *testing.T, version uint32, plannerCap int64) *evalHub {
	t.Helper()
	clock := testenv.NewFakeClock(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
	keyStore := testenv.NewKeyStore()
	keySvc, err := keys.New(keyStore, clock, testenv.Random{})
	if err != nil {
		t.Fatal(err)
	}
	h := &evalHub{provider: testenv.NewFakeAgentProvider(), store: testenv.NewQuotaStore(), keys: map[shared.APIKeyID]string{}, version: version}
	for _, label := range []string{"eval-tester", "other-tester"} {
		issued, err := keySvc.Issue(t.Context(), label)
		if err != nil {
			t.Fatal(err)
		}
		h.keys[shared.APIKeyID(issued.ID)] = issued.Key
		if h.owner == "" {
			h.owner = shared.APIKeyID(issued.ID)
		}
	}

	models, err := modelallowlist.New([]modelallowlist.Entry{
		{Alias: "default", Model: "claude-strong", MaxOutput: 4096},
		{Alias: "planner", Model: "claude-planner", MaxOutput: plannerCap},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.sealer, err = testenv.Sealer(1); err != nil {
		t.Fatal(err)
	}
	if h.system, h.catalog, err = testenv.Panel(version); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	svc, err := agentstep.New(agentstep.Deps{
		Accounts: h.store, Turns: testenv.NewAgentTurns(), Provider: h.provider, Limiter: &testenv.FakeLimiter{}, Clock: clock,
		Random: testenv.Random{}, Models: models, Sealer: h.sealer, Log: log,
	}, agentstep.Config{
		Unit: "tokens", Limit: 1_000_000, ReservationTTL: 10 * time.Minute, KeyRetention: 24 * time.Hour, CallTimeout: time.Minute,
		Profiles: []agentstep.Profile{{Name: values.ProfilePanel, Alias: "planner", Versions: []agentstep.Version{{System: h.system, Catalog: h.catalog}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := keyauth.New(keyStore)
	if err != nil {
		t.Fatal(err)
	}
	gh, err := connect.NewGateway(noCompleter{}, log, connect.WithAgentStep(svc))
	if err != nil {
		t.Fatal(err)
	}
	router, err := connect.NewRouter(connect.Deps{Log: log, Readiness: readyProbe{}, Auth: auth, Gateway: gh})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	h.client = hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)
	return h
}

func (h *evalHub) other() shared.APIKeyID {
	for id := range h.keys {
		if id != h.owner {
			return id
		}
	}
	return ""
}

// evalRequest is a step of thread th-eval with the given turns (default: one student text).
func evalRequest(key string, turns ...*hubv1.AgentTurn) *hubv1.AgentStepRequest {
	if len(turns) == 0 {
		turns = []*hubv1.AgentTurn{{Turn: &hubv1.AgentTurn_StudentText{StudentText: "что задали на неделю?"}}}
	}
	return &hubv1.AgentStepRequest{
		IdempotencyKey: key, ThreadId: evalThread, Profile: hubv1.AgentProfile_AGENT_PROFILE_PANEL, CatalogVersion: 1,
		Base:  &hubv1.AgentBase{Subjects: []*hubv1.AgentSubject{{SubjectId: "s1", Name: "Физика"}}, Today: "2026-10-22", TimeZone: "Europe/Moscow"},
		Scope: &hubv1.AgentScope{SubjectId: "s1", Screen: hubv1.AgentScreen_AGENT_SCREEN_ASSIGNMENTS},
		Turns: turns,
	}
}

func (h *evalHub) stepAs(t *testing.T, owner shared.APIKeyID, msg *hubv1.AgentStepRequest) (*hubv1.AgentStepResponse, error) {
	t.Helper()
	req := connectrpc.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+h.keys[owner])
	resp, err := h.client.AgentStep(t.Context(), req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (h *evalHub) step(t *testing.T, msg *hubv1.AgentStepRequest) (*hubv1.AgentStepResponse, error) {
	t.Helper()
	return h.stepAs(t, h.owner, msg)
}

// answerWith is a provider answer with the given calls; calls the domain cannot even build are a malformed answer.
func answerWith(t *testing.T, calls ...[3]string) testenv.AgentProviderStep {
	t.Helper()
	u, err := values.NewUsage(900, 60)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]values.ToolCall, 0, len(calls))
	for _, c := range calls {
		call, err := values.NewToolCall(c[0], c[1], c[2])
		if err != nil {
			return testenv.AgentProviderStep{Answer: values.MalformedAgentAnswer(u)}
		}
		out = append(out, call)
	}
	return testenv.AgentProviderStep{Answer: values.NewAgentAnswer(out, u, true)}
}

// echoTurns is the history after resp: the student's text, the sealed calls and one result per call.
func echoTurns(text string, resp *hubv1.AgentStepResponse, result string) []*hubv1.AgentTurn {
	results := &hubv1.ToolResults{}
	for _, c := range resp.GetCalls().GetCalls() {
		results.Results = append(results.Results, &hubv1.ToolResult{CallId: c.GetCallId(), ResultJson: result})
	}
	return []*hubv1.AgentTurn{
		{Turn: &hubv1.AgentTurn_StudentText{StudentText: text}},
		{Turn: &hubv1.AgentTurn_Calls{Calls: resp.GetCalls()}},
		{Turn: &hubv1.AgentTurn_Results{Results: results}},
	}
}

func modelCallsOf(t *testing.T, resp *hubv1.AgentStepResponse) []values.ToolCall {
	t.Helper()
	var out []values.ToolCall
	for _, c := range resp.GetCalls().GetCalls() {
		call, err := values.NewToolCall(c.GetCallId(), c.GetTool(), c.GetArgumentsJson())
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, call)
	}
	return out
}

func (h *evalHub) binding(owner shared.APIKeyID) seal.Binding {
	return seal.Binding{Owner: owner, ThreadID: evalThread, Profile: values.ProfilePanel, CatalogVersion: h.version}
}

// --- fixtures -------------------------------------------------------------------------------------------------------

type evalReplayFixture struct {
	Name  string `json:"name"`
	Calls []struct {
		CallID        string `json:"call_id"`
		Tool          string `json:"tool"`
		ArgumentsJSON string `json:"arguments_json"`
	} `json:"calls"`
	Hub string `json:"hub"`
}

type evalInjectionFixture struct {
	Name      string `json:"name"`
	Untrusted string `json:"untrusted"`
}

func loadEvalFixtures[T any](t *testing.T, dir string) []T {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(codeRoot(t), "shared", "testdata", "agent", dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures in shared/testdata/agent/%s (%v)", dir, err)
	}
	slices.Sort(files)
	out := make([]T, 0, len(files))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var v T
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		out = append(out, v)
	}
	return out
}

// --- checks ---------------------------------------------------------------------------------------------------------

// TestAgentEvalHubReplayFixtures: a recorded answer the catalog accepts comes back unchanged and sealed for the caller
// and the thread; one it refuses is a non-retryable PROVIDER_ERROR. In both cases the same key answers again without
// a second provider call (free replay, or the stored failure).
func TestAgentEvalHubReplayFixtures(t *testing.T) {
	for _, fx := range loadEvalFixtures[evalReplayFixture](t, "replay") {
		t.Run(fx.Name, func(t *testing.T) {
			h := newEvalHub(t)
			calls := make([][3]string, 0, len(fx.Calls))
			for _, c := range fx.Calls {
				calls = append(calls, [3]string{c.CallID, c.Tool, c.ArgumentsJSON})
			}
			h.provider.Push(answerWith(t, calls...))

			for attempt := range 2 {
				resp, err := h.step(t, evalRequest("ag:th-eval:1:1"))
				switch fx.Hub {
				case "accept":
					if err != nil {
						t.Fatalf("attempt %d: %v", attempt, err)
					}
					got := resp.GetCalls().GetCalls()
					if len(got) != len(fx.Calls) || resp.GetReplayed() != (attempt == 1) {
						t.Fatalf("attempt %d: response %v", attempt, resp)
					}
					for i, c := range fx.Calls {
						if got[i].GetCallId() != c.CallID || got[i].GetTool() != c.Tool || got[i].GetArgumentsJson() != c.ArgumentsJSON {
							t.Fatalf("call %d changed on the way: %v", i, got[i])
						}
					}
					if err := h.sealer.Verify(h.binding(h.owner), modelCallsOf(t, resp), resp.GetCalls().GetSeal()); err != nil {
						t.Fatalf("seal: %v", err)
					}
				case "reject":
					code, d := gwDetail(t, err)
					if code != connectrpc.CodeUnavailable || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR || d.GetRetryable() {
						t.Fatalf("attempt %d: %v / %v, want a permanent PROVIDER_ERROR", attempt, code, d)
					}
				default:
					t.Fatalf("unknown expectation %q", fx.Hub)
				}
			}
			if n := len(h.provider.Calls()); n != 1 {
				t.Fatalf("provider calls = %d, want the repeat served from the ledger", n)
			}
		})
	}
}

// TestAgentEvalHubForgedSeal: an echo whose calls or binding differ from what the Hub sealed is invalid_argument
// before the ledger or the provider is touched.
func TestAgentEvalHubForgedSeal(t *testing.T) {
	h := newEvalHub(t)
	h.provider.Push(answerWith(t, [3]string{"toolu_01", "find_lessons", `{"subject":"s1"}`}))
	first, err := h.step(t, evalRequest("ag:th-eval:1:1"))
	if err != nil {
		t.Fatal(err)
	}
	ledger := len(h.store.Ledger(h.owner))
	foreign, err := testenv.Sealer(7) // a seal made with a secret the Hub does not have
	if err != nil {
		t.Fatal(err)
	}

	mutate := func(f func(sc *hubv1.SealedCalls)) *hubv1.AgentStepRequest {
		turns := echoTurns("что задали на неделю?", first, `{"lessons":[]}`)
		sc := turns[1].GetCalls()
		forged := &hubv1.SealedCalls{Seal: bytes.Clone(sc.GetSeal())}
		for _, c := range sc.GetCalls() {
			forged.Calls = append(forged.Calls, &hubv1.ToolCall{CallId: c.GetCallId(), Tool: c.GetTool(), ArgumentsJson: c.GetArgumentsJson()})
		}
		f(forged)
		turns[1] = &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_Calls{Calls: forged}}
		return evalRequest("ag:th-eval:1:2", turns...)
	}
	otherThread := evalRequest("ag:th-eval:1:2", echoTurns("что задали на неделю?", first, `{}`)...)
	otherThread.ThreadId = "th-other"

	cases := map[string]struct {
		owner shared.APIKeyID
		msg   *hubv1.AgentStepRequest
	}{
		"changed argument":  {h.owner, mutate(func(sc *hubv1.SealedCalls) { sc.Calls[0].ArgumentsJson = `{"subject":"s2"}` })},
		"changed tool":      {h.owner, mutate(func(sc *hubv1.SealedCalls) { sc.Calls[0].Tool = "list_subjects"; sc.Calls[0].ArgumentsJson = `{}` })},
		"flipped seal byte": {h.owner, mutate(func(sc *hubv1.SealedCalls) { sc.Seal[len(sc.Seal)-1] ^= 1 })},
		"truncated seal":    {h.owner, mutate(func(sc *hubv1.SealedCalls) { sc.Seal = sc.Seal[:8] })},
		"added call": {h.owner, mutate(func(sc *hubv1.SealedCalls) {
			sc.Calls = append(sc.Calls, &hubv1.ToolCall{CallId: "toolu_99", Tool: "list_subjects", ArgumentsJson: "{}"})
		})},
		"another thread": {h.owner, otherThread},
		"another tester": {h.other(), evalRequest("ag:th-eval:1:2", echoTurns("что задали на неделю?", first, `{}`)...)},
		"seal of a foreign key": {h.owner, mutate(func(sc *hubv1.SealedCalls) {
			sc.Calls[0].ArgumentsJson = `{"subject":"s2"}`
			sc.Seal = foreign.Seal(h.binding(h.owner), []values.ToolCall{mustToolCall(t, sc.Calls[0])})
		})},
	}
	for name, c := range cases {
		if name == "added call" {
			// The results must name every echoed call, or the request is refused for its shape before the seal.
			c.msg.Turns[2].GetResults().Results = append(c.msg.Turns[2].GetResults().Results, &hubv1.ToolResult{CallId: "toolu_99", ResultJson: "{}"})
		}
		_, err := h.stepAs(t, c.owner, c.msg)
		var ce *connectrpc.Error
		if !errors.As(err, &ce) || ce.Code() != connectrpc.CodeInvalidArgument || ce.Message() != "the seal of echoed calls does not match" {
			t.Errorf("%s: err = %v, want the seal refused", name, err)
		}
	}
	if len(h.provider.Calls()) != 1 || len(h.store.Ledger(h.owner)) != ledger || len(h.store.Ledger(h.other())) != 0 {
		t.Fatal("a forged echo reached the provider or the ledger")
	}
}

func mustToolCall(t *testing.T, c *hubv1.ToolCall) values.ToolCall {
	t.Helper()
	call, err := values.NewToolCall(c.GetCallId(), c.GetTool(), c.GetArgumentsJson())
	if err != nil {
		t.Fatal(err)
	}
	return call
}

// golden catalog argument types, for the sweep.
type evalCatalog struct {
	Tools []struct {
		Name      string `json:"name"`
		Arguments []struct {
			Name     string   `json:"name"`
			Type     string   `json:"type"`
			Required bool     `json:"required"`
			Values   []string `json:"values"`
		} `json:"arguments"`
	} `json:"tools"`
}

func evalBaseline(typ string, enum []string) any {
	switch typ {
	case "id":
		return "x1"
	case "ids", "card_ids":
		return []string{"x1"}
	case "date":
		return "2026-10-22"
	case "enum":
		return enum[0]
	case "label", "query", "text":
		return "a"
	case "text_list":
		return []string{"a"}
	case "plan_steps":
		return []any{map[string]any{"tool": "list_subjects"}}
	}
	return []any{map[string]any{"kind": "text", "question": "q"}} // questions
}

func evalInjected(typ, text string) any {
	switch typ {
	case "ids", "card_ids", "text_list":
		return []string{text}
	case "plan_steps":
		return []any{map[string]any{"tool": "propose_send", "arguments": map[string]any{"assignment": "a1", "version": text}}}
	case "questions":
		return []any{map[string]any{"kind": "text", "question": text}}
	}
	return text
}

// TestAgentEvalHubInjectionSweep puts each untrusted text into every argument of every tool and runs the Hub's check
// of returned calls. Enum and date arguments never take it; an ID argument takes it only when it has the shape of an
// ID (no spaces): such a call reaches the desktop, whose typed provenance check refuses it
// (desktop/tests/agent_injection_test.go, same fixtures). Labels, queries, card texts and plan step arguments are free
// text for the Hub; the desktop checks them. The sweep runs on the current catalog (v3): the assignment IDs a reply
// lists (card_ids) take the text only in the shape of an ID, like any ID.
func TestAgentEvalHubInjectionSweep(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(codeRoot(t), "shared", "testdata", "tool_catalog_v3.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cat evalCatalog
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatal(err)
	}
	_, catalog, err := testenv.Panel(3)
	if err != nil {
		t.Fatal(err)
	}
	check := func(tool string, args map[string]any) error {
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		call, err := values.NewToolCall("toolu_01", tool, string(b))
		if err != nil {
			return err
		}
		return catalog.Check(call)
	}

	for _, fx := range loadEvalFixtures[evalInjectionFixture](t, "injection") {
		for _, tool := range cat.Tools {
			base := map[string]any{}
			for _, a := range tool.Arguments {
				if a.Required {
					base[a.Name] = evalBaseline(a.Type, a.Values)
				}
			}
			if err := check(tool.Name, base); err != nil {
				t.Fatalf("%s: the baseline call is refused: %v", tool.Name, err)
			}
			for _, a := range tool.Arguments {
				args := map[string]any{}
				for k, v := range base {
					args[k] = v
				}
				args[a.Name] = evalInjected(a.Type, fx.Untrusted)
				err := check(tool.Name, args)
				switch a.Type {
				case "enum", "date":
					if err == nil {
						t.Errorf("%s: %s.%s (%s) took the untrusted text", fx.Name, tool.Name, a.Name, a.Type)
					}
				case "id", "ids", "card_ids":
					if (err == nil) != values.ValidID(fx.Untrusted) {
						t.Errorf("%s: %s.%s: accepted %v, want only an ID-shaped text", fx.Name, tool.Name, a.Name, err == nil)
					}
				}
			}
		}
	}
}

// TestAgentEvalHubInjectionThroughTheStep: the untrusted text arrives as the student's text and inside a tool result.
// The provider gets it only in the history, never in the system prompt, the tool list, the base or the scope; and
// when the provider obeys it with a call the catalog does not allow (an address argument), the Hub answers a
// permanent PROVIDER_ERROR and returns no call.
func TestAgentEvalHubInjectionThroughTheStep(t *testing.T) {
	for _, fx := range loadEvalFixtures[evalInjectionFixture](t, "injection") {
		t.Run(fx.Name, func(t *testing.T) {
			h := newEvalHub(t)
			q, err := json.Marshal(fx.Untrusted)
			if err != nil {
				t.Fatal(err)
			}
			h.provider.Push(
				answerWith(t, [3]string{"toolu_01", "inspect_upload", `{"upload":"u1"}`}),
				answerWith(t, [3]string{"toolu_02", "propose_send", `{"assignment":"a1","version":"v1","to":` + string(q) + `}`}),
			)
			first, err := h.step(t, evalRequest("ag:th-eval:1:1", &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_StudentText{StudentText: fx.Untrusted}}))
			if err != nil {
				t.Fatal(err)
			}
			_, err = h.step(t, evalRequest("ag:th-eval:1:2", echoTurns(fx.Untrusted, first, `{"note":`+string(q)+`}`)...))
			code, d := gwDetail(t, err)
			if code != connectrpc.CodeUnavailable || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR || d.GetRetryable() {
				t.Fatalf("obedient call: %v / %v, want a permanent PROVIDER_ERROR", code, d)
			}

			calls := h.provider.Calls()
			if len(calls) != 2 {
				t.Fatalf("provider calls = %d", len(calls))
			}
			for i, c := range calls {
				if c.System() != h.system || fmt.Sprint(c.Tools()) != fmt.Sprint(calls[0].Tools()) || len(c.Tools()) != len(h.catalog.Specs()) {
					t.Fatalf("call %d: the system prompt or the tool list changed", i)
				}
				if strings.Contains(c.Base(), fx.Untrusted) || strings.Contains(c.Scope(), fx.Untrusted) {
					t.Fatalf("call %d: the untrusted text is in the base or the scope", i)
				}
			}
			turns := calls[1].Turns()
			if len(turns) != 3 || turns[0].Text() != fx.Untrusted || !strings.Contains(turns[2].Results()[0].JSON(), "note") {
				t.Fatalf("history = %v", turns)
			}
		})
	}
}

// --- catalog v3: the beta questions (TASK-172, TASK-173) ------------------------------------------------------------

// v3OutputCap is the output cap the desktop asks for a planner step since catalog 3 (agentturn.MaxOutputTokens).
const v3OutputCap = 4096

// v3Request is a step of thread th-eval on catalog 3 with the desktop's output cap.
func v3Request(key string, turns ...*hubv1.AgentTurn) *hubv1.AgentStepRequest {
	r := evalRequest(key, turns...)
	r.CatalogVersion, r.MaxOutputTokens = 3, v3OutputCap
	return r
}

// v3Case is a question of the beta with the recorded answers of a planner that follows the v3 prompt: the calls of
// each step and the result the desktop sends back for the calls of each step but the last. rule is the line of the
// v3 prompt the answers follow; the recorded texts are impersonal (no «я», «ты», «вы», no greeting).
type v3Case struct {
	name, text, rule string
	steps            [][][3]string
	results          []string
}

// TestAgentEvalHubV3BetaQuestions runs the four questions of the beta and a work on a topic without an assignment
// through the real AgentStep on catalog 3 (recorded answers; a live model is level 2, desktop/tests/agenteval): the
// prompt holds the rule each answer follows, every answer comes back unchanged and sealed for catalog 3, a reply
// lists assignment IDs only (the desktop draws the titles), and the provider gets the v3 prompt, the v3 tools and
// the desktop's output cap.
func TestAgentEvalHubV3BetaQuestions(t *testing.T) {
	assignments := `{"assignments":[{"id":"a-12","due":"2026-10-24","status":"upcoming"},{"id":"a-20","due":"2026-10-27","status":"upcoming"}]}`
	cases := []v3Case{
		{name: "greeting", text: "Привет, ты тут?", rule: "No greetings and no address",
			steps: [][][3]string{{{"toolu_01", "reply", `{"text":"Можно спросить про задания, конспекты или лекции."}`}}}},
		{name: "where are the drafts", text: "Где посмотреть черновики?",
			rule:  "drafts of assignments: «Задания», the page of the assignment, block «Решение»",
			steps: [][][3]string{{{"toolu_01", "reply", `{"text":"Черновик — в «Задания», на странице задания, блок «Решение»."}`}}}},
		{name: "what is due this week", text: "Что горит на неделе?",
			rule: "That reply lists the assignments in its assignments argument",
			steps: [][][3]string{
				{{"toolu_01", "list_assignments", `{"due_before":"2026-10-28","status":"upcoming"}`}},
				{{"toolu_02", "reply", `{"text":"Два задания, ближайший срок — 2026-10-24.","assignments":["a-12","a-20"]}`}},
			}, results: []string{assignments}},
		{name: "help with practicals 2-4", text: "Помоги с пр 2-4", rule: "When the request is ambiguous, call ask_questions",
			steps: [][][3]string{
				{{"toolu_01", "list_assignments", `{"status":"upcoming"}`}},
				{{"toolu_02", "ask_questions", `{"questions":[{"kind":"choice","question":"Какие работы имеются в виду?",` +
					`"options":["Практические 2-4 по физике","Другие"],"default":0}]}`}},
			}, results: []string{assignments}},
		{name: "topic without an assignment", text: "Сделай презентацию на тему «Реформы Петра I»",
			rule: "A work on a topic (реферат, доклад, эссе, презентация) is the app's own job",
			steps: [][][3]string{
				{{"toolu_01", "list_assignments", `{}`}},
				{{"toolu_02", "reply", `{"text":"Работа по теме начинается с задания в «Задания»; задания с этой темой нет."}`}},
			}, results: []string{`{"assignments":[]}`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newEvalHubOf(t, 3, v3OutputCap)
			if !strings.Contains(h.system, c.rule) {
				t.Fatalf("the v3 prompt has no rule %q", c.rule)
			}
			for _, s := range c.steps {
				h.provider.Push(answerWith(t, s...))
			}
			turns := []*hubv1.AgentTurn{{Turn: &hubv1.AgentTurn_StudentText{StudentText: c.text}}}
			for i, s := range c.steps {
				resp, err := h.step(t, v3Request(fmt.Sprintf("ag:th-eval:1:%d", i+1), turns...))
				if err != nil {
					t.Fatalf("step %d: %v", i+1, err)
				}
				got := resp.GetCalls().GetCalls()
				if len(got) != len(s) {
					t.Fatalf("step %d: %d calls, want %d", i+1, len(got), len(s))
				}
				for j, want := range s {
					if got[j].GetCallId() != want[0] || got[j].GetTool() != want[1] || got[j].GetArgumentsJson() != want[2] {
						t.Fatalf("step %d: call %d changed on the way: %v", i+1, j, got[j])
					}
				}
				if err := h.sealer.Verify(h.binding(h.owner), modelCallsOf(t, resp), resp.GetCalls().GetSeal()); err != nil {
					t.Fatalf("step %d: seal: %v", i+1, err)
				}
				if i < len(c.results) {
					results := &hubv1.ToolResults{}
					for _, call := range got {
						results.Results = append(results.Results, &hubv1.ToolResult{CallId: call.GetCallId(), ResultJson: c.results[i]})
					}
					turns = append(turns, &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_Calls{Calls: resp.GetCalls()}},
						&hubv1.AgentTurn{Turn: &hubv1.AgentTurn_Results{Results: results}})
				}
			}

			calls := h.provider.Calls()
			if len(calls) != len(c.steps) {
				t.Fatalf("provider calls = %d", len(calls))
			}
			for i, pc := range calls {
				if pc.System() != h.system || pc.MaxOutput() != v3OutputCap || !replyListsAssignments(pc.Tools()) {
					t.Fatalf("provider call %d: prompt, cap %d or tools are not those of catalog 3", i, pc.MaxOutput())
				}
			}
		})
	}
}

// replyListsAssignments reports whether the reply tool of a tool list takes an assignments argument (catalog 3).
func replyListsAssignments(tools []values.ToolSpec) bool {
	for _, tl := range tools {
		if tl.Name() == "reply" {
			return strings.Contains(tl.Schema(), `"assignments"`)
		}
	}
	return false
}

// TestAgentEvalHubV3ReplyAssignmentsShape: a reply lists at most 10 IDs and only on catalog 3. A title or an object
// instead of an ID, eleven IDs, or a list on catalog 2 is a permanent PROVIDER_ERROR: no such call reaches the desktop.
func TestAgentEvalHubV3ReplyAssignmentsShape(t *testing.T) {
	ids := make([]string, 0, toolcatalog.MaxCardIDs+1)
	for i := range toolcatalog.MaxCardIDs + 1 {
		ids = append(ids, fmt.Sprintf("a%d", i))
	}
	eleven, err := json.Marshal(ids)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		version uint32
		args    string
	}{
		"title instead of an id": {3, `{"text":"Одно задание.","assignments":["Эссе по истории"]}`},
		"object with a title":    {3, `{"text":"Одно задание.","assignments":[{"id":"a1","title":"Эссе"}]}`},
		"eleven ids":             {3, `{"text":"Много заданий.","assignments":` + string(eleven) + `}`},
		"a list on catalog 2":    {2, `{"text":"Одно задание.","assignments":["a1"]}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newEvalHubOf(t, c.version, v3OutputCap)
			h.provider.Push(answerWith(t, [3]string{"toolu_01", "reply", c.args}))
			req := v3Request("ag:th-eval:1:1")
			req.CatalogVersion = c.version
			_, err := h.step(t, req)
			code, d := gwDetail(t, err)
			if code != connectrpc.CodeUnavailable || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR || d.GetRetryable() {
				t.Fatalf("%v / %v, want a permanent PROVIDER_ERROR", code, d)
			}
		})
	}
}

// --- INSPECT_MATERIAL reader (TASK-122) -----------------------------------------------------------------------------

// readerHub is the public listener with Complete wired to the real gateway use case and the embedded
// ops/inspect_material, ops/triage_assignment and ops/answer prompts; the provider and the quota store are fakes.
type readerHub struct {
	provider     *testenv.FakeProvider
	store        *testenv.QuotaStore
	prompt       string
	triagePrompt string
	answerPrompt string
	client       hubv1connect.GatewayServiceClient
	key          string
	owner        shared.APIKeyID
}

func newReaderHub(t *testing.T) *readerHub {
	t.Helper()
	clock := testenv.NewFakeClock(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
	keyStore := testenv.NewKeyStore()
	keySvc, err := keys.New(keyStore, clock, testenv.Random{})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := keySvc.Issue(t.Context(), "reader-tester")
	if err != nil {
		t.Fatal(err)
	}
	version, prompt, err := prompts.Op(string(values.OperationInspectMaterial))
	if err != nil {
		t.Fatal(err)
	}
	triageVersion, triagePrompt, err := prompts.Op(string(values.OperationTriageAssignment))
	if err != nil {
		t.Fatal(err)
	}
	answerVersion, answerPrompt, err := prompts.Op(string(values.OperationAnswer))
	if err != nil {
		t.Fatal(err)
	}
	h := &readerHub{provider: testenv.NewFakeProvider(), store: testenv.NewQuotaStore(), prompt: string(prompt),
		triagePrompt: string(triagePrompt), answerPrompt: string(answerPrompt), key: issued.Key, owner: shared.APIKeyID(issued.ID)}
	models, err := modelallowlist.New([]modelallowlist.Entry{
		{Alias: "default", Model: "claude-strong", MaxOutput: 4096},
		{Alias: "planner", Model: "claude-planner", MaxOutput: 512},
	})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	svc, err := gateway.New(gateway.Deps{
		Accounts: h.store, Results: h.store, Provider: h.provider, Limiter: &testenv.FakeLimiter{}, Clock: clock,
		Random: testenv.Random{}, Models: models, Log: log,
	}, gateway.Config{
		Unit: "tokens", Limit: 1_000_000, ReservationTTL: 10 * time.Minute, KeyRetention: 24 * time.Hour, CallTimeout: time.Minute,
		ServerPrompts: map[values.Operation]gateway.ServerPrompt{
			values.OperationInspectMaterial:  {Alias: "planner", Version: fmt.Sprintf("v%d", version), Text: h.prompt},
			values.OperationTriageAssignment: {Alias: "default", Version: fmt.Sprintf("v%d", triageVersion), Text: h.triagePrompt},
			values.OperationAnswer:           {Alias: "default", Version: fmt.Sprintf("v%d", answerVersion), Text: h.answerPrompt},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := keyauth.New(keyStore)
	if err != nil {
		t.Fatal(err)
	}
	gh, err := connect.NewGateway(svc, log)
	if err != nil {
		t.Fatal(err)
	}
	router, err := connect.NewRouter(connect.Deps{Log: log, Readiness: readyProbe{}, Auth: auth, Gateway: gh})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	h.client = hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)
	return h
}

func (h *readerHub) inspect(t *testing.T, key, material string) (*hubv1.CompletionResult, error) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"today":         "2026-10-22",
		"subjects":      []map[string]string{{"id": "s1", "name": "Физика"}, {"id": "s2", "name": "История"}},
		"material_text": material,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := connectrpc.NewRequest(&hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey: key, OperationType: hubv1.OperationType_OPERATION_TYPE_INSPECT_MATERIAL, UntrustedData: string(data),
	}})
	req.Header().Set("Authorization", "Bearer "+h.key)
	resp, err := h.client.Complete(t.Context(), req)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetResult(), nil
}

func readerAnswer(t *testing.T, output string) testenv.ProviderStep {
	t.Helper()
	u, err := values.NewUsage(700, 30)
	if err != nil {
		t.Fatal(err)
	}
	c, err := values.NewCompletion(output, u, values.StopEnd)
	if err != nil {
		t.Fatal(err)
	}
	return testenv.ProviderStep{Completion: c}
}

const readerValid = `{"doc_kind":"lecture","subject":"s1","lesson_date":"2026-10-14","has_homework":true,"has_ticket_list":false,"mentions_email":true}`

// TestAgentEvalReaderValidAnswer: a valid answer comes back as the canonical schema object through the handler.
func TestAgentEvalReaderValidAnswer(t *testing.T) {
	h := newReaderHub(t)
	h.provider.Push(readerAnswer(t, readerValid))
	res, err := h.inspect(t, "insp:u1", "Лекция 5, 14 октября. ДЗ: задачи 1-3, прислать на x@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if res.GetOutput() != readerValid || res.GetReplayed() {
		t.Fatalf("result = %v", res)
	}
	call := h.provider.Calls()[0]
	if call.Instructions() != h.prompt || call.Model() != "claude-planner" || strings.Contains(call.Instructions(), "Лекция") {
		t.Fatalf("provider call = %v", call)
	}
}

// TestAgentEvalReaderEnumFreeText enforces every injection fixture, reader-enum-free-text first: the material says
// what an obedient model would write, and the model obeys in each slot of the schema (the text in an enum, ID, date or
// flag field, or as an extra field). The Hub refuses the whole answer with a permanent PROVIDER_ERROR, the text never
// reaches the instruction slot, and the same key repeats the refusal without a second provider call.
func TestAgentEvalReaderEnumFreeText(t *testing.T) {
	fixtures := loadEvalFixtures[evalInjectionFixture](t, "injection")
	if !slices.ContainsFunc(fixtures, func(f evalInjectionFixture) bool { return f.Name == "reader-enum-free-text" }) {
		t.Fatal("fixture reader-enum-free-text is missing")
	}
	for _, fx := range fixtures {
		q, err := json.Marshal(fx.Untrusted)
		if err != nil {
			t.Fatal(err)
		}
		slots := map[string]string{
			"doc_kind":       strings.Replace(readerValid, `"lecture"`, string(q), 1),
			"subject":        strings.Replace(readerValid, `"s1"`, string(q), 1),
			"lesson_date":    strings.Replace(readerValid, `"2026-10-14"`, string(q), 1),
			"mentions_email": strings.Replace(readerValid, `"mentions_email":true`, `"mentions_email":`+string(q), 1),
			"extra field":    strings.Replace(readerValid, `}`, `,"note":`+string(q)+`}`, 1),
			"prose":          fx.Untrusted,
		}
		for slot, out := range slots {
			t.Run(fx.Name+"/"+slot, func(t *testing.T) {
				h := newReaderHub(t)
				h.provider.Push(readerAnswer(t, out))
				for attempt := range 2 {
					res, err := h.inspect(t, "insp:u1", fx.Untrusted)
					code, d := gwDetail(t, err)
					if res != nil || code != connectrpc.CodeUnavailable || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR || d.GetRetryable() {
						t.Fatalf("attempt %d: %v / %v, want a permanent PROVIDER_ERROR", attempt, code, d)
					}
					if strings.Contains(err.Error(), fx.Untrusted) {
						t.Fatalf("the error quotes the answer: %v", err)
					}
				}
				calls := h.provider.Calls()
				if len(calls) != 1 || calls[0].Instructions() != h.prompt || strings.Contains(calls[0].Instructions(), fx.Untrusted) {
					t.Fatalf("provider calls = %d; the untrusted text must stay in the data", len(calls))
				}
				if used, held := h.store.Totals(h.owner); used != 730 || held != 0 {
					t.Fatalf("used %d held %d, want the billed answer charged once", used, held)
				}
			})
		}
	}
}
