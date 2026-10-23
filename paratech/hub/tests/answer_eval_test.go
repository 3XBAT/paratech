package tests

// ANSWER reader (TASK-118), level 1: every injection fixture of shared/testdata/agent as a lesson fragment, through the
// real router, key check, Complete handler, gateway and the embedded ops/answer prompt.

import (
	"encoding/json"
	"strings"
	"testing"

	connectrpc "connectrpc.com/connect"

	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

const answerValid = `{"claims":[{"text":"Импульс сохраняется в замкнутой системе.",` +
	`"cites":[{"chunk_id":"rec-1:t00:10:00","quote":"импульс замкнутой системы сохраняется"}]}],"missing":[]}`

func (h *readerHub) answer(t *testing.T, key, bearer, fragment string) (*hubv1.CompletionResult, error) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"question": "Что сохраняется в замкнутой системе?",
		"fragments": []map[string]string{
			{"chunk_id": "rec-1:t00:10:00", "text": "Закон: импульс замкнутой системы сохраняется."},
			{"chunk_id": "note-1:s2", "text": fragment},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := connectrpc.NewRequest(&hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey: key, OperationType: hubv1.OperationType_OPERATION_TYPE_ANSWER, UntrustedData: string(data),
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

// TestAgentEvalAnswerInjectionStaysData: each fixture as a fragment reaches the model only inside the data on the
// server prompt and the strong alias; the answer comes back canonical.
func TestAgentEvalAnswerInjectionStaysData(t *testing.T) {
	for _, fx := range loadEvalFixtures[evalInjectionFixture](t, "injection") {
		t.Run(fx.Name, func(t *testing.T) {
			h := newReaderHub(t)
			h.provider.Push(readerAnswer(t, "```json\n"+answerValid+"\n```"))
			res, err := h.answer(t, "ans:th1:1", h.key, fx.Untrusted)
			if err != nil {
				t.Fatal(err)
			}
			if res.GetOutput() != answerValid || res.GetReplayed() {
				t.Fatalf("answer = %v", res)
			}
			call := h.provider.Calls()[0]
			var data struct {
				Fragments []struct {
					Text string `json:"text"`
				} `json:"fragments"`
			}
			if err := json.Unmarshal([]byte(call.Data()), &data); err != nil || len(data.Fragments) != 2 || data.Fragments[1].Text != fx.Untrusted ||
				call.Instructions() != h.answerPrompt || call.Model() != "claude-strong" || strings.Contains(call.Instructions(), fx.Untrusted) {
				t.Fatalf("provider call = %v (%v); the untrusted text must stay in the data", call, err)
			}
		})
	}
}

// TestAgentEvalAnswerObeyedAnswerIsRefused: the model obeys the fixture in a slot that is not claim text (a chunk ID,
// an extra field, a claim without a citation) or answers in prose: a permanent PROVIDER_ERROR, charged once, replayed.
func TestAgentEvalAnswerObeyedAnswerIsRefused(t *testing.T) {
	for _, fx := range loadEvalFixtures[evalInjectionFixture](t, "injection") {
		q, err := json.Marshal(fx.Untrusted)
		if err != nil {
			t.Fatal(err)
		}
		slots := map[string]string{
			"chunk_id":    strings.Replace(answerValid, `"chunk_id":"rec-1:t00:10:00"`, `"chunk_id":`+string(q), 1),
			"extra field": strings.Replace(answerValid, `"missing":[]`, `"missing":[],"action":`+string(q), 1),
			"no citation": `{"claims":[{"text":` + string(q) + `,"cites":[]}],"missing":[]}`,
			"prose":       fx.Untrusted,
		}
		for slot, out := range slots {
			t.Run(fx.Name+"/"+slot, func(t *testing.T) {
				h := newReaderHub(t)
				h.provider.Push(readerAnswer(t, out))
				for attempt := range 2 {
					res, err := h.answer(t, "ans:th1:1", h.key, fx.Untrusted)
					code, d := gwDetail(t, err)
					if res != nil || code != connectrpc.CodeUnavailable || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR || d.GetRetryable() {
						t.Fatalf("attempt %d: %v / %v, want a permanent PROVIDER_ERROR", attempt, code, d)
					}
					if strings.Contains(err.Error(), fx.Untrusted) {
						t.Fatalf("the error quotes the answer: %v", err)
					}
				}
				if len(h.provider.Calls()) != 1 {
					t.Fatalf("provider calls = %d, want the refusal replayed", len(h.provider.Calls()))
				}
				if used, held := h.store.Totals(h.owner); used != 730 || held != 0 {
					t.Fatalf("used %d held %d, want the billed answer charged once", used, held)
				}
			})
		}
	}
}

// TestAgentEvalAnswerNeedsAKey: without the tester's key the call is unauthenticated and reaches nothing.
func TestAgentEvalAnswerNeedsAKey(t *testing.T) {
	h := newReaderHub(t)
	for _, bearer := range []string{"", "pt_live_obviously-fake"} {
		if _, err := h.answer(t, "ans:th1:1", bearer, "Фрагмент"); connectrpc.CodeOf(err) != connectrpc.CodeUnauthenticated {
			t.Fatalf("bearer %q: err = %v, want unauthenticated", bearer, err)
		}
	}
	if len(h.provider.Calls()) != 0 || len(h.store.Ledger(h.owner)) != 0 {
		t.Fatal("an unauthenticated call reached the provider or the ledger")
	}
}
