package tests

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/adapters/anthropic"
	"paratech/hub/internal/adapters/postgres"
	"paratech/hub/internal/adapters/ratelimit"
	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/cases/keyauth"
	"paratech/hub/internal/cases/keys"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/services/requestlimits"
	"paratech/hub/internal/domain/llm/services/seal"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/internal/input/connect"
	"paratech/hub/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// The adapters and the fake implement the ports of the agent step use case.
var (
	_ agentstep.Provider       = (*anthropic.Client)(nil)
	_ agentstep.Provider       = (*testenv.FakeAgentProvider)(nil)
	_ agentstep.Accounts       = (*postgres.QuotaAccounts)(nil)
	_ agentstep.AssistantTurns = (*postgres.AgentTurns)(nil)
	_ agentstep.AssistantTurns = (*testenv.AgentTurns)(nil)
	_ agentstep.RateLimiter    = (*ratelimit.Limiter)(nil)
)

// agStudentText must never reach a log or a stored row; model output (the calls) is stored on purpose.
const agStudentText = "agent-marker: what is due in calculus"

// agentEnv is the real Hub path for AgentStep and Complete on a fresh Postgres schema: router with API-key auth,
// gateway handler with both use cases, quota ledger and limiter. Only the providers and the clock are fakes.
type agentEnv struct {
	quotaEnv
	provider *testenv.FakeAgentProvider
	sealer   *seal.Sealer
	client   hubv1connect.GatewayServiceClient
	owner    shared.APIKeyID
	apiKey   string
	logs     *lockedBuffer
}

func newAgentEnv(t *testing.T, steps ...testenv.AgentProviderStep) agentEnv {
	t.Helper()
	e := agentEnv{quotaEnv: newQuotaEnv(t, 1_000_000), provider: testenv.NewFakeAgentProvider(steps...), logs: &lockedBuffer{}}
	log := slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	keySvc, err := keys.New(e.db.APIKeys(), e.clock, testenv.Random{})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := keySvc.Issue(t.Context(), "agent-tester")
	if err != nil {
		t.Fatal(err)
	}
	e.owner, e.apiKey = shared.APIKeyID(issued.ID), issued.Key

	models, err := modelallowlist.New([]modelallowlist.Entry{
		{Alias: "default", Model: "claude-test-model", MaxOutput: gwModelCap},
		{Alias: "planner", Model: "claude-planner-model", MaxOutput: 400},
	})
	if err != nil {
		t.Fatal(err)
	}
	rate, err := requestlimits.NewRate(requestlimits.MaxPerMinute, requestlimits.MaxBurst)
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := ratelimit.New(rate)
	if err != nil {
		t.Fatal(err)
	}
	if e.sealer, err = testenv.Sealer(1); err != nil {
		t.Fatal(err)
	}
	system, catalog, err := testenv.PanelV1()
	if err != nil {
		t.Fatal(err)
	}
	agentSvc, err := agentstep.New(agentstep.Deps{
		Accounts: e.db.QuotaAccounts(), Turns: e.db.AgentTurns(), Provider: e.provider, Limiter: limiter, Clock: e.clock, Random: testenv.Random{},
		Models: models, Sealer: e.sealer, Log: log,
	}, agentstep.Config{
		Unit: "tokens", Limit: 1_000_000, ReservationTTL: quotaTTL, KeyRetention: gwRetention, CallTimeout: 4 * time.Minute,
		Profiles: []agentstep.Profile{{Name: values.ProfilePanel, Alias: "planner", Versions: []agentstep.Version{{System: system, Catalog: catalog}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	gatewaySvc, err := gateway.New(gateway.Deps{
		Accounts: e.db.QuotaAccounts(), Results: e.db.QuotaAccounts(), Provider: testenv.NewFakeProvider(gwAnswer(t, gwOutput, 1, 1)),
		Limiter: limiter, Clock: e.clock, Random: testenv.Random{}, Models: models, Log: log,
	}, gateway.Config{Unit: "tokens", Limit: 1_000_000, ReservationTTL: quotaTTL, KeyRetention: gwRetention, CallTimeout: 4 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	auth, err := keyauth.New(e.db.APIKeys())
	if err != nil {
		t.Fatal(err)
	}
	gh, err := connect.NewGateway(gatewaySvc, log, connect.WithAgentStep(agentSvc))
	if err != nil {
		t.Fatal(err)
	}
	router, err := connect.NewRouter(connect.Deps{Log: log, Readiness: e.db, Auth: auth, Gateway: gh})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	e.client = hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)
	return e
}

func agStepRequest(key string, turns ...*hubv1.AgentTurn) *hubv1.AgentStepRequest {
	if len(turns) == 0 {
		turns = []*hubv1.AgentTurn{{Turn: &hubv1.AgentTurn_StudentText{StudentText: agStudentText}}}
	}
	return &hubv1.AgentStepRequest{
		IdempotencyKey: key, ThreadId: "thread-1", Profile: hubv1.AgentProfile_AGENT_PROFILE_PANEL, CatalogVersion: 1,
		Base:  &hubv1.AgentBase{Subjects: []*hubv1.AgentSubject{{SubjectId: "subj-1", Name: "Calculus"}}, Today: "2026-10-22", TimeZone: "Europe/Moscow"},
		Scope: &hubv1.AgentScope{SubjectId: "subj-1", Screen: hubv1.AgentScreen_AGENT_SCREEN_TODAY},
		Turns: turns,
	}
}

func (e agentEnv) step(ctx context.Context, msg *hubv1.AgentStepRequest) (*hubv1.AgentStepResponse, error) {
	req := connectrpc.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.client.AgentStep(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func agAnswer(t *testing.T, calls ...[3]string) testenv.AgentProviderStep {
	t.Helper()
	u, err := values.NewUsage(900, 60)
	if err != nil {
		t.Fatal(err)
	}
	var out []values.ToolCall
	for _, c := range calls {
		call, err := values.NewToolCall(c[0], c[1], c[2])
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, call)
	}
	return testenv.AgentProviderStep{Answer: values.NewAgentAnswer(out, u, true)}
}

// echoOf is the history of the next step: the student's text, the sealed calls of resp and a result per call.
func echoOf(resp *hubv1.AgentStepResponse) []*hubv1.AgentTurn {
	results := &hubv1.ToolResults{}
	for _, c := range resp.GetCalls().GetCalls() {
		results.Results = append(results.Results, &hubv1.ToolResult{CallId: c.GetCallId(), ResultJson: `{"lessons":[]}`})
	}
	return []*hubv1.AgentTurn{
		{Turn: &hubv1.AgentTurn_StudentText{StudentText: agStudentText}},
		{Turn: &hubv1.AgentTurn_Calls{Calls: resp.GetCalls()}},
		{Turn: &hubv1.AgentTurn_Results{Results: results}},
	}
}

// TestAgentStepAgainstPostgres: a step is reserved, called, settled and stored; the sealed answer echoes into the
// next step; a retry replays without a charge; no request text is stored or logged.
func TestAgentStepAgainstPostgres(t *testing.T) {
	e := newAgentEnv(t,
		agAnswer(t, [3]string{"toolu_01", "find_lessons", `{"subject":"subj-1"}`}),
		agAnswer(t, [3]string{"toolu_02", "reply", `{"text":"Nothing is due."}`}))

	first, err := e.step(t.Context(), agStepRequest("ag:thread-1:1:1"))
	if err != nil {
		t.Fatal(err)
	}
	calls := first.GetCalls().GetCalls()
	if len(calls) != 1 || calls[0].GetTool() != "find_lessons" || len(first.GetCalls().GetSeal()) != seal.Size || first.GetReplayed() ||
		first.GetUsage().GetInputTokens() != 900 || first.GetQuota().GetRemaining().GetValue() != 1_000_000-960 {
		t.Fatalf("first step = %v", first)
	}

	second, err := e.step(t.Context(), agStepRequest("ag:thread-1:1:2", echoOf(first)...))
	if err != nil {
		t.Fatalf("echo of a sealed answer: %v", err)
	}
	if second.GetCalls().GetCalls()[0].GetTool() != "reply" {
		t.Fatalf("second step = %v", second)
	}

	again, err := e.step(t.Context(), agStepRequest("ag:thread-1:1:1"))
	if err != nil || !again.GetReplayed() || !bytes.Equal(again.GetCalls().GetSeal(), first.GetCalls().GetSeal()) ||
		again.GetReservationId() != first.GetReservationId() {
		t.Fatalf("replay = %v, %v", again, err)
	}
	if len(e.provider.Calls()) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(e.provider.Calls()))
	}
	if used, held := e.assertConsistent(t, e.owner); used != 1920 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}

	var ops []string
	rows, err := e.conn.Query(t.Context(), "SELECT operation_type || '/' || model_alias FROM quota_idempotency_keys WHERE api_key_id = $1 ORDER BY 1",
		e.owner.String())
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		ops = append(ops, s)
	}
	rows.Close()
	if strings.Join(ops, ",") != "agent_step/planner,agent_step/planner" {
		t.Fatalf("key records = %v", ops)
	}
	for _, table := range []string{"quota_accounts", "quota_reservations", "quota_idempotency_keys", "quota_ledger", "api_keys", "agent_turns"} {
		var n int
		if err := e.conn.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" t WHERE t::text LIKE '%agent-marker%' OR t::text LIKE '%Calculus%'").
			Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d rows hold request text (%v)", table, n, err)
		}
	}
	for _, secret := range []string{"agent-marker", "subj-1", "Calculus", "Nothing is due", e.apiKey} {
		if strings.Contains(e.logs.String(), secret) {
			t.Fatalf("the log contains %q", secret)
		}
	}
}

// TestAgentStepForgedSealAgainstPostgres: an echo with altered calls or another thread is invalid_argument and
// writes nothing.
func TestAgentStepForgedSealAgainstPostgres(t *testing.T) {
	e := newAgentEnv(t, agAnswer(t, [3]string{"toolu_01", "find_lessons", `{"subject":"subj-1"}`}))
	first, err := e.step(t.Context(), agStepRequest("ag:1"))
	if err != nil {
		t.Fatal(err)
	}
	before := e.ledgerKinds(t, e.owner)

	forged := echoOf(first)
	forged[1] = &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_Calls{Calls: &hubv1.SealedCalls{
		Calls: []*hubv1.ToolCall{{CallId: "toolu_01", Tool: "find_lessons", ArgumentsJson: `{"subject":"subj-2"}`}},
		Seal:  first.GetCalls().GetSeal(),
	}}}
	otherThread := agStepRequest("ag:3", echoOf(first)...)
	otherThread.ThreadId = "thread-2"
	for name, msg := range map[string]*hubv1.AgentStepRequest{"altered call": agStepRequest("ag:2", forged...), "other thread": otherThread} {
		_, err := e.step(t.Context(), msg)
		var ce *connectrpc.Error
		if !errors.As(err, &ce) || ce.Code() != connectrpc.CodeInvalidArgument || ce.Message() != "the seal of echoed calls does not match" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if after := e.ledgerKinds(t, e.owner); len(after) != len(before) || after["reserve"] != before["reserve"] {
		t.Fatalf("ledger changed: %v -> %v", before, after)
	}
	if len(e.provider.Calls()) != 1 {
		t.Fatalf("provider calls = %d", len(e.provider.Calls()))
	}
}

// TestAgentStepAndCompleteKeysNeverMatch: one key string used for Complete and then AgentStep is a conflict, never a
// replay of the other RPC's answer (fingerprints are domain-separated, ADR-0023).
func TestAgentStepAndCompleteKeysNeverMatch(t *testing.T) {
	e := newAgentEnv(t, agAnswer(t, [3]string{"toolu_01", "list_subjects", `{}`}))
	req := connectrpc.NewRequest(&hubv1.CompleteRequest{Spec: gwSpec("shared-key")})
	req.Header().Set("Authorization", "Bearer "+e.apiKey)
	if _, err := e.client.Complete(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	_, err := e.step(t.Context(), agStepRequest("shared-key"))
	code, detail := gwDetail(t, err)
	if code != connectrpc.CodeFailedPrecondition || detail.GetCode() != hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT {
		t.Fatalf("AgentStep with a Complete key: %v", err)
	}
	if len(e.provider.Calls()) != 0 {
		t.Fatal("the agent provider was called")
	}
}

// TestAgentStepTurnGoneAgainstPostgres: every returned call set keeps a turn row of its owner; when the row is gone,
// the continuation is failed_precondition without a provider call or a charge, and a replay stays free.
func TestAgentStepTurnGoneAgainstPostgres(t *testing.T) {
	e := newAgentEnv(t, agAnswer(t, [3]string{"toolu_01", "find_lessons", `{"subject":"subj-1"}`}))
	first, err := e.step(t.Context(), agStepRequest("ag:1"))
	if err != nil {
		t.Fatal(err)
	}
	if n := e.rowCount(t, "agent_turns", e.owner); n != 1 {
		t.Fatalf("kept turns = %d", n)
	}
	used, _ := e.assertConsistent(t, e.owner)

	if _, err := e.conn.Exec(t.Context(), "DELETE FROM agent_turns WHERE api_key_id = $1", e.owner.String()); err != nil {
		t.Fatal(err)
	}
	_, err = e.step(t.Context(), agStepRequest("ag:2", echoOf(first)...))
	var ce *connectrpc.Error
	if !errors.As(err, &ce) || ce.Code() != connectrpc.CodeFailedPrecondition {
		t.Fatalf("continuation without the kept turn: err = %v", err)
	}
	if after, held := e.assertConsistent(t, e.owner); after != used || held != 0 {
		t.Fatalf("used %d -> %d, held %d", used, after, held)
	}
	if again, err := e.step(t.Context(), agStepRequest("ag:1")); err != nil || !again.GetReplayed() {
		t.Fatalf("replay = %v, %v", again, err)
	}
	if len(e.provider.Calls()) != 1 {
		t.Fatalf("provider calls = %d", len(e.provider.Calls()))
	}
}

func TestAgentStepInvalidRequestsAgainstPostgres(t *testing.T) {
	e := newAgentEnv(t)
	version := agStepRequest("ag:1")
	version.CatalogVersion = 9
	big := agStepRequest("ag:2", &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_StudentText{StudentText: strings.Repeat("a", values.MaxStudentTextBytes+1)}})
	noProfile := agStepRequest("ag:3")
	noProfile.Profile = hubv1.AgentProfile_AGENT_PROFILE_UNSPECIFIED
	for name, c := range map[string]struct {
		msg  *hubv1.AgentStepRequest
		want string
	}{
		"unknown catalog version": {version, "unknown tool catalog version"},
		"student text over 4 KiB": {big, "invalid agent step request"},
		"no profile":              {noProfile, "invalid agent step request"},
	} {
		_, err := e.step(t.Context(), c.msg)
		var ce *connectrpc.Error
		if !errors.As(err, &ce) || ce.Code() != connectrpc.CodeInvalidArgument || ce.Message() != c.want {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if n := e.rowCount(t, "quota_ledger", e.owner); n != 0 {
		t.Fatalf("invalid requests wrote %d ledger rows", n)
	}
}
