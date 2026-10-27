package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"

	"paratech/hub/internal/adapters/anthropic"
	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/cases/keyauth"
	"paratech/hub/internal/cases/keys"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/internal/input/connect"
	"paratech/hub/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

type alwaysReady struct{}

func (alwaysReady) Ping(context.Context) error { return nil }

// thinkingEnv is the Hub path of AgentStep with the real anthropic adapter against a Messages API fake that demands
// the thinking block back, the way the 5.x models do with adaptive thinking. Stores are in memory, so it runs without
// Postgres.
type thinkingEnv struct {
	api    *testenv.FakeMessagesAPI
	turns  *testenv.AgentTurns
	store  *testenv.QuotaStore
	client hubv1connect.GatewayServiceClient
	owner  shared.APIKeyID
	apiKey string
	logs   *lockedBuffer
}

func newThinkingEnv(t *testing.T, answers ...[]testenv.FakeToolUse) thinkingEnv {
	t.Helper()
	return newPanelEnv(t, []uint32{1}, answers...)
}

// newPanelEnv is thinkingEnv serving the panel catalogs of versions.
func newPanelEnv(t *testing.T, versions []uint32, answers ...[]testenv.FakeToolUse) thinkingEnv {
	t.Helper()
	e := thinkingEnv{api: testenv.NewFakeMessagesAPI(t, answers...), turns: testenv.NewAgentTurns(), logs: &lockedBuffer{}}
	log := slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	clock := testenv.NewFakeClock(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))

	keyStore := testenv.NewKeyStore()
	keySvc, err := keys.New(keyStore, clock, testenv.Random{})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := keySvc.Issue(t.Context(), "thinking-tester")
	if err != nil {
		t.Fatal(err)
	}
	e.owner, e.apiKey = shared.APIKeyID(issued.ID), issued.Key

	models, err := modelallowlist.New([]modelallowlist.Entry{
		{Alias: "default", Model: "claude-test-model", MaxOutput: 1000},
		{Alias: "planner", Model: "claude-planner-model", MaxOutput: 400},
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := anthropic.New(anthropic.Options{APIKey: providerTestKey, BaseURL: e.api.URL, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	sealer, err := testenv.Sealer(1)
	if err != nil {
		t.Fatal(err)
	}
	var served []agentstep.Version
	for _, v := range versions {
		system, catalog, err := testenv.Panel(v)
		if err != nil {
			t.Fatal(err)
		}
		served = append(served, agentstep.Version{System: system, Catalog: catalog})
	}
	store := testenv.NewQuotaStore()
	e.store = store
	agentSvc, err := agentstep.New(agentstep.Deps{
		Accounts: store, Turns: e.turns, Provider: provider, Limiter: &testenv.FakeLimiter{}, Clock: clock,
		Random: testenv.Random{}, Models: models, Sealer: sealer, Log: log,
	}, agentstep.Config{
		Unit: "tokens", Limit: 1_000_000, ReservationTTL: 10 * time.Minute, KeyRetention: 24 * time.Hour, CallTimeout: 4 * time.Minute,
		Profiles: []agentstep.Profile{{Name: values.ProfilePanel, Alias: "planner", Versions: served}},
	})
	if err != nil {
		t.Fatal(err)
	}
	gatewaySvc, err := gateway.New(gateway.Deps{
		Accounts: store, Results: store, Provider: testenv.NewFakeProvider(), Limiter: &testenv.FakeLimiter{}, Clock: clock,
		Random: testenv.Random{}, Models: models, Log: log,
	}, gateway.Config{Unit: "tokens", Limit: 1_000_000, ReservationTTL: 10 * time.Minute, KeyRetention: 24 * time.Hour, CallTimeout: 4 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := keyauth.New(keyStore)
	if err != nil {
		t.Fatal(err)
	}
	gh, err := connect.NewGateway(gatewaySvc, log, connect.WithAgentStep(agentSvc))
	if err != nil {
		t.Fatal(err)
	}
	router, err := connect.NewRouter(connect.Deps{Log: log, Readiness: alwaysReady{}, Auth: auth, Gateway: gh})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	e.client = hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)
	return e
}

func (e thinkingEnv) step(ctx context.Context, msg *hubv1.AgentStepRequest) (*hubv1.AgentStepResponse, error) {
	req := connectrpc.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.client.AgentStep(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// assertNoThinking fails when a thinking text or signature reached the response or the log.
func (e thinkingEnv) assertNoThinking(t *testing.T, resp *hubv1.AgentStepResponse) {
	t.Helper()
	encoded, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{string(encoded), e.logs.String()} {
		if strings.Contains(s, testenv.FakeThinkingMarker) || strings.Contains(s, testenv.FakeSignatureMarker) {
			t.Fatalf("thinking leaked: %s", s)
		}
	}
}

// TestAgentStepKeepsTheThinkingTurn: a continuation reaches the provider with the model's own turn, thinking and
// signature unchanged, so the provider accepts it; the desktop never sees the thinking. When the kept turn is gone
// the step fails with a clear permanent error and no provider call, and a replay stays free.
func TestAgentStepKeepsTheThinkingTurn(t *testing.T) {
	e := newThinkingEnv(t,
		[]testenv.FakeToolUse{{ID: "toolu_01", Name: "find_lessons", Input: `{"subject":"subj-1"}`}},
		[]testenv.FakeToolUse{{ID: "toolu_02", Name: "reply", Input: `{"text":"Nothing is due."}`}})

	first, err := e.step(t.Context(), agStepRequest("ag:1"))
	if err != nil {
		t.Fatal(err)
	}
	e.assertNoThinking(t, first)

	second, err := e.step(t.Context(), agStepRequest("ag:2", echoOf(first)...))
	if err != nil {
		t.Fatalf("continuation: %v (rejected by the API: %d)", err, e.api.Rejected())
	}
	if second.GetCalls().GetCalls()[0].GetTool() != "reply" || e.api.Rejected() != 0 {
		t.Fatalf("second = %v", second)
	}
	e.assertNoThinking(t, second)
	reqs := e.api.Requests()
	if !bytes.Contains(reqs[1], []byte(testenv.FakeSignatureMarker+"-1-")) {
		t.Fatal("the continuation did not carry the kept thinking block")
	}
	var sent struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(reqs[1], &sent); err != nil || len(sent.Messages) != 3 {
		t.Fatalf("messages = %d, %v", len(sent.Messages), err)
	}

	e.turns.Drop(e.owner)
	_, err = e.step(t.Context(), agStepRequest("ag:3", echoOf(first)...))
	var ce *connectrpc.Error
	if !errors.As(err, &ce) || ce.Code() != connectrpc.CodeFailedPrecondition || !strings.Contains(ce.Message(), "no longer keeps") {
		t.Fatalf("continuation without the kept turn: err = %v", err)
	}
	if len(e.api.Requests()) != 2 {
		t.Fatalf("provider requests = %d, want no call without the kept turn", len(e.api.Requests()))
	}

	again, err := e.step(t.Context(), agStepRequest("ag:2", echoOf(first)...))
	if err != nil || !again.GetReplayed() || len(e.api.Requests()) != 2 {
		t.Fatalf("replay = %v, %v", again, err)
	}
	e.assertNoThinking(t, again)
}
