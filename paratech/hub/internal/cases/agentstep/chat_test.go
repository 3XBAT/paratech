package agentstep_test

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/testenv"
)

// chatService serves panel catalogs 8 and 9 with a provider that answers every step with one reply.
func chatService(t *testing.T) (*agentstep.Service, *testenv.FakeAgentProvider) {
	t.Helper()
	var versions []agentstep.Version
	for _, v := range []uint32{8, 9} {
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
	steps := make([]testenv.AgentProviderStep, 0, 16)
	for range 16 {
		steps = append(steps, answer(t, 100, 20, toolCall(t, "toolu_01", "reply", `{"text":"Готово."}`)))
	}
	provider := testenv.NewFakeAgentProvider(steps...)
	svc, err := agentstep.New(agentstep.Deps{
		Accounts: testenv.NewQuotaStore(), Turns: testenv.NewAgentTurns(), Provider: provider, Limiter: &testenv.FakeLimiter{},
		Clock: testenv.NewFakeClock(t0), Random: testenv.Random{}, Models: models(t), Sealer: sealer, Log: slog.New(slog.DiscardHandler),
	}, agentstep.Config{Unit: "tokens", Limit: 1_000_000, ReservationTTL: ttl, KeyRetention: retention, CallTimeout: callTimeout,
		Profiles: []agentstep.Profile{{Name: values.ProfilePanel, Alias: "planner", Versions: versions}}})
	if err != nil {
		t.Fatal(err)
	}
	return svc, provider
}

// chatCommand is a catalog 9 request with a summary, an earlier pair, the current text and two mentions.
func chatCommand(key string) agentstep.Command {
	cmd := command(alice, key,
		agentstep.Turn{Kind: values.TurnHistorySummary, Summary: "Earlier: limits in Матан."},
		agentstep.Turn{Kind: values.TurnStudentText, StudentText: "что такое предел?"},
		agentstep.Turn{Kind: values.TurnAgentReply, AgentReply: "reply: Предел — это …"},
		agentstep.Turn{Kind: values.TurnStudentText, StudentText: "что задали по @Лекция 3?"},
	)
	cmd.CatalogVersion = values.ChatCatalogVersion
	cmd.Scope.Mentions = []agentstep.Mention{
		{Kind: values.MentionLesson, ID: "les-phys-3", Current: true},
		{Kind: values.MentionSubject, ID: "subj-2", Current: false},
	}
	return cmd
}

// TestChatRequestReachesTheProvider: on catalog 9 the mentions go into the scope of the call (kinds and IDs) and the
// summary is the first turn; the call uses the chat layout.
func TestChatRequestReachesTheProvider(t *testing.T) {
	svc, provider := chatService(t)
	if _, err := svc.Step(t.Context(), chatCommand("ag:1")); err != nil {
		t.Fatal(err)
	}
	call := provider.Calls()[0]
	if !call.ChatLayout() || !strings.Contains(call.Scope(), `{"kind":"lesson","id":"les-phys-3","current":true}`) ||
		!strings.Contains(call.Scope(), `{"kind":"subject","id":"subj-2","current":false}`) {
		t.Fatalf("scope = %s, chat layout %v", call.Scope(), call.ChatLayout())
	}
	turns := call.Turns()
	if len(turns) != 4 || turns[0].Kind() != values.TurnHistorySummary || turns[0].Text() != "Earlier: limits in Матан." {
		t.Fatalf("turns = %v", turns)
	}
	if strings.Contains(call.Scope(), "Earlier") {
		t.Fatal("the summary leaked into the scope")
	}
}

// TestChatFieldsRefused: every broken chat field is invalid_argument before any ledger row or provider call.
func TestChatFieldsRefused(t *testing.T) {
	sum := agentstep.Turn{Kind: values.TurnHistorySummary, Summary: "Earlier: limits."}
	text := agentstep.Turn{Kind: values.TurnStudentText, StudentText: "и что?"}
	reply := agentstep.Turn{Kind: values.TurnAgentReply, AgentReply: "reply: Готово."}
	seventeen := make([]agentstep.Mention, values.MaxAgentMentions+1)
	for i := range seventeen {
		seventeen[i] = agentstep.Mention{Kind: values.MentionMaterial, ID: fmt.Sprintf("mat-%d", i)}
	}
	for name, mutate := range map[string]func(*agentstep.Command){
		"summary on catalog 8":  func(c *agentstep.Command) { c.CatalogVersion = 8 },
		"mentions on catalog 8": func(c *agentstep.Command) { c.CatalogVersion = 8; c.Turns = c.Turns[1:] },
		"summary not first":     func(c *agentstep.Command) { c.Turns = []agentstep.Turn{text, reply, sum, text} },
		"summary last":          func(c *agentstep.Command) { c.Turns = []agentstep.Turn{text, sum} },
		"summary before reply":  func(c *agentstep.Command) { c.Turns = []agentstep.Turn{sum, reply, text} },
		"summary alone":         func(c *agentstep.Command) { c.Turns = []agentstep.Turn{sum} },
		"summary over 8 KiB": func(c *agentstep.Command) {
			c.Turns[0].Summary = strings.Repeat("a", values.MaxHistorySummaryBytes+1)
		},
		"blank summary":        func(c *agentstep.Command) { c.Turns[0].Summary = " " },
		"17 mentions":          func(c *agentstep.Command) { c.Scope.Mentions = seventeen },
		"unknown mention kind": func(c *agentstep.Command) { c.Scope.Mentions[0].Kind = "recording" },
		"no mention kind":      func(c *agentstep.Command) { c.Scope.Mentions[0].Kind = "" },
		"bad mention id":       func(c *agentstep.Command) { c.Scope.Mentions[0].ID = "les 3" },
		"empty mention id":     func(c *agentstep.Command) { c.Scope.Mentions[0].ID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			svc, provider := chatService(t)
			cmd := chatCommand("ag:1")
			mutate(&cmd)
			if _, err := svc.Step(t.Context(), cmd); !errors.Is(err, agentstep.ErrInvalidRequest) {
				t.Fatalf("err = %v, want ErrInvalidRequest", err)
			}
			if n := len(provider.Calls()); n != 0 {
				t.Fatalf("the provider was called %d times", n)
			}
		})
	}

	// Catalog 8 without the chat fields is served as before.
	svc, _ := chatService(t)
	cmd := command(alice, "ag:2")
	cmd.CatalogVersion = 8
	if _, err := svc.Step(t.Context(), cmd); err != nil {
		t.Fatalf("catalog 8: %v", err)
	}
}

// TestChatIdempotency: a retry of the same request replays without a second provider call; the same key with other
// mentions or another summary is a conflict, because both are part of the fingerprint.
func TestChatIdempotency(t *testing.T) {
	svc, provider := chatService(t)
	first, err := svc.Step(t.Context(), chatCommand("ag:1"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Step(t.Context(), chatCommand("ag:1"))
	if err != nil || !again.Replayed || again.Calls[0] != first.Calls[0] || len(provider.Calls()) != 1 {
		t.Fatalf("retry: %+v, %v, provider calls %d", again, err, len(provider.Calls()))
	}
	for name, mutate := range map[string]func(*agentstep.Command){
		"other mention":   func(c *agentstep.Command) { c.Scope.Mentions[0].ID = "les-phys-4" },
		"not current":     func(c *agentstep.Command) { c.Scope.Mentions[0].Current = false },
		"no mentions":     func(c *agentstep.Command) { c.Scope.Mentions = nil },
		"other summary":   func(c *agentstep.Command) { c.Turns[0].Summary = "Earlier: something else." },
		"without summary": func(c *agentstep.Command) { c.Turns = c.Turns[1:] },
	} {
		cmd := chatCommand("ag:1")
		mutate(&cmd)
		if _, err := svc.Step(t.Context(), cmd); !errors.Is(err, agentstep.ErrIdempotencyConflict) {
			t.Errorf("%s: err = %v, want ErrIdempotencyConflict", name, err)
		}
	}
	if len(provider.Calls()) != 1 {
		t.Fatalf("provider calls = %d", len(provider.Calls()))
	}
}

// TestChatConcurrentRetries: concurrent copies of one chat request make one provider call; the others replay it or
// find the attempt running.
func TestChatConcurrentRetries(t *testing.T) {
	svc, provider := chatService(t)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = svc.Step(t.Context(), chatCommand("ag:1"))
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil && !errors.Is(err, agentstep.ErrAttemptRunning) {
			t.Errorf("step %d: %v", i, err)
		}
	}
	if n := len(provider.Calls()); n != 1 {
		t.Fatalf("provider calls = %d, want 1", n)
	}
}
