package tests

// CHAT_SUMMARY reader (TASK-267, ADR-0028), level 1: every injection fixture of shared/testdata/agent as the previous
// summary, a student text and a reply, through the real router, key check, Complete handler, gateway and the embedded
// prompt.

import (
	"encoding/json"
	"strings"
	"testing"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/domain/llm/services/chatsummary"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// summaryCap is the chat summary's own output cap in readerHub, above the planner alias cap (512) there.
const summaryCap = 4096

const summaryValid = `{"summary":"Обсуждали задание по физике.\nОткрыто: задача 5."}`

func (h *readerHub) chatSummary(t *testing.T, key, bearer, data string) (*hubv1.CompletionResult, error) {
	t.Helper()
	req := connectrpc.NewRequest(&hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey: key, OperationType: hubv1.OperationType_OPERATION_TYPE_CHAT_SUMMARY, UntrustedData: data,
	}})
	if bearer != "" {
		req.Header().Set("Authorization", "Bearer "+bearer)
	}
	resp, err := h.client.Complete(t.Context(), req)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetResult(), nil
}

func summaryData(t *testing.T, previous string, pairs ...chatsummary.Pair) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"summary": previous, "pairs": pairs})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestAgentEvalChatSummaryInjectionStaysData: each fixture as the previous summary, a student text and a reply reaches
// the model only inside the data, on the server prompt and the planner alias; the answer comes back canonical and a
// retry with the key (cs:<thread>:<through>:<hash8>) replays it without a provider call or a new charge.
func TestAgentEvalChatSummaryInjectionStaysData(t *testing.T) {
	for _, fx := range loadEvalFixtures[evalInjectionFixture](t, "injection") {
		t.Run(fx.Name, func(t *testing.T) {
			h := newReaderHub(t)
			h.provider.Push(readerAnswer(t, summaryValid))
			data := summaryData(t, fx.Untrusted, chatsummary.Pair{Student: fx.Untrusted, Reply: fx.Untrusted},
				chatsummary.Pair{Student: "Что задали по физике?", Reply: ""})

			for attempt, replayed := range []bool{false, true} {
				res, err := h.chatSummary(t, "cs:th-1:10", h.key, data)
				if err != nil {
					t.Fatalf("attempt %d: %v", attempt, err)
				}
				if res.GetOutput() != summaryValid || res.GetReplayed() != replayed {
					t.Fatalf("attempt %d = %v", attempt, res)
				}
			}
			calls := h.provider.Calls()
			if len(calls) != 1 {
				t.Fatalf("provider calls = %d, want the replay free", len(calls))
			}
			var sent struct {
				Summary string             `json:"summary"`
				Pairs   []chatsummary.Pair `json:"pairs"`
			}
			call := calls[0]
			if err := json.Unmarshal([]byte(call.Data()), &sent); err != nil || sent.Summary != fx.Untrusted || sent.Pairs[0].Student != fx.Untrusted ||
				call.Instructions() != h.summaryPrompt || call.Model() != "claude-planner" || call.MaxOutput() != 512 ||
				strings.Contains(call.Instructions(), fx.Untrusted) {
				t.Fatalf("provider call = %v (%v); the untrusted text must stay in the data", call, err)
			}
			if used, held := h.store.Totals(h.owner); used != 730 || held != 0 {
				t.Fatalf("used %d held %d, want one charge", used, held)
			}
		})
	}
}

// TestAgentEvalChatSummaryObeyedAnswerIsRefused: the model obeys the fixture (an extra field, prose, the fixture as a
// key) both times: a permanent PROVIDER_ERROR, the two billed answers charged once, the refusal replayed by key.
func TestAgentEvalChatSummaryObeyedAnswerIsRefused(t *testing.T) {
	for _, fx := range loadEvalFixtures[evalInjectionFixture](t, "injection") {
		q, err := json.Marshal(fx.Untrusted)
		if err != nil {
			t.Fatal(err)
		}
		for slot, out := range map[string]string{
			"extra field": strings.Replace(summaryValid, `"}`, `","action":`+string(q)+`}`, 1),
			"as a key":    `{` + string(q) + `:"x"}`,
			"prose":       fx.Untrusted,
		} {
			t.Run(fx.Name+"/"+slot, func(t *testing.T) {
				h := newReaderHub(t)
				h.provider.Push(readerAnswer(t, out), readerAnswer(t, out))
				data := summaryData(t, "", chatsummary.Pair{Student: fx.Untrusted, Reply: ""})
				for attempt := range 2 {
					res, err := h.chatSummary(t, "cs:th-1:10", h.key, data)
					code, d := gwDetail(t, err)
					if res != nil || code != connectrpc.CodeUnavailable || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR || d.GetRetryable() {
						t.Fatalf("attempt %d: %v / %v, want a permanent PROVIDER_ERROR", attempt, code, d)
					}
					if strings.Contains(err.Error(), fx.Untrusted) {
						t.Fatalf("the error quotes the answer: %v", err)
					}
				}
				if len(h.provider.Calls()) != 2 {
					t.Fatalf("provider calls = %d, want the answer, one reask and the refusal replayed", len(h.provider.Calls()))
				}
				if used, held := h.store.Totals(h.owner); used != 1460 || held != 0 {
					t.Fatalf("used %d held %d, want both billed answers charged once", used, held)
				}
			})
		}
	}
}

// TestAgentEvalChatSummaryLimits: an input over a limit is invalid_argument before the ledger and the provider; without
// the tester's key the call is unauthenticated and reaches nothing.
func TestAgentEvalChatSummaryLimits(t *testing.T) {
	h := newReaderHub(t)
	pair := chatsummary.Pair{Student: "q", Reply: "r"}
	many := make([]chatsummary.Pair, chatsummary.MaxPairs+1)
	for i := range many {
		many[i] = pair
	}
	for name, data := range map[string]string{
		"no pairs":         `{"summary":"","pairs":[]}`,
		"eleven pairs":     summaryData(t, "", many...),
		"long student":     summaryData(t, "", chatsummary.Pair{Student: strings.Repeat("s", chatsummary.MaxStudentBytes+1)}),
		"long reply":       summaryData(t, "", chatsummary.Pair{Student: "q", Reply: strings.Repeat("r", chatsummary.MaxReplyBytes+1)}),
		"long summary":     summaryData(t, strings.Repeat("s", chatsummary.MaxPreviousBytes+1), pair),
		"data over 64 KiB": summaryData(t, "", pair) + strings.Repeat(" ", chatsummary.MaxDataBytes),
	} {
		if _, err := h.chatSummary(t, "cs:th-1:10", h.key, data); connectrpc.CodeOf(err) != connectrpc.CodeInvalidArgument {
			t.Errorf("%s: err = %v, want invalid_argument", name, err)
		}
	}
	for _, bearer := range []string{"", "pt_live_obviously-fake"} {
		if _, err := h.chatSummary(t, "cs:th-1:10", bearer, summaryData(t, "", pair)); connectrpc.CodeOf(err) != connectrpc.CodeUnauthenticated {
			t.Errorf("bearer %q: err = %v, want unauthenticated", bearer, err)
		}
	}
	if len(h.provider.Calls()) != 0 || len(h.store.Ledger(h.owner)) != 0 {
		t.Fatal("a refused call reached the provider or the ledger")
	}
}
