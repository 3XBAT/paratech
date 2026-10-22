package tests

// TRIAGE_ASSIGNMENT reader (TASK-129), level 1: every injection fixture of shared/testdata/agent as the assignment and
// the material text, through the real router, key check, Complete handler, gateway and the embedded prompt.

import (
	"encoding/json"
	"strings"
	"testing"

	connectrpc "connectrpc.com/connect"

	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

const triageValid = `{"work_type":"essay",` +
	`"requirements":{"volume":"3 страницы","file_format":"docx","formatting":"","variant":"","deadline":"","sources":""},` +
	`"feasibility":"full","feasibility_reason":"",` +
	`"questions":[{"kind":"choice","question":"Какой стиль?","options":["Научный","Публицистический"],"default":0,"why":"Тон текста."}],` +
	`"assumptions":[],"helper":null}`

func (h *readerHub) triage(t *testing.T, key, bearer string, round int, assignment, material string) (*hubv1.CompletionResult, error) {
	t.Helper()
	previous := []map[string]string{}
	if round == 2 {
		previous = append(previous, map[string]string{"question": "Какой стиль?", "answer": assignment})
	}
	data, err := json.Marshal(map[string]any{
		"round": round, "assignment_text": assignment, "material_text": material, "previous": previous,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := connectrpc.NewRequest(&hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey: key, OperationType: hubv1.OperationType_OPERATION_TYPE_TRIAGE_ASSIGNMENT, UntrustedData: string(data),
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

// TestAgentEvalTriageInjectionStaysData: each fixture as the assignment, the material and a previous answer reaches
// the model only inside the data on the server prompt and strong alias; the answer comes back canonical, and in
// round 2 without questions.
func TestAgentEvalTriageInjectionStaysData(t *testing.T) {
	for _, fx := range loadEvalFixtures[evalInjectionFixture](t, "injection") {
		t.Run(fx.Name, func(t *testing.T) {
			h := newReaderHub(t)
			h.provider.Push(readerAnswer(t, triageValid), readerAnswer(t, triageValid))

			res, err := h.triage(t, "tr:a1:1", h.key, 1, fx.Untrusted, fx.Untrusted)
			if err != nil {
				t.Fatal(err)
			}
			if res.GetOutput() != triageValid || res.GetReplayed() {
				t.Fatalf("round 1 = %v", res)
			}
			res, err = h.triage(t, "tr:a1:2", h.key, 2, fx.Untrusted, "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(res.GetOutput(), `"questions":[]`) {
				t.Fatalf("round 2 = %s, want no questions", res.GetOutput())
			}

			for _, call := range h.provider.Calls() {
				var data struct {
					AssignmentText string `json:"assignment_text"`
				}
				if err := json.Unmarshal([]byte(call.Data()), &data); err != nil || data.AssignmentText != fx.Untrusted ||
					call.Instructions() != h.triagePrompt || call.Model() != "claude-strong" || strings.Contains(call.Instructions(), fx.Untrusted) {
					t.Fatalf("provider call = %v (%v); the untrusted text must stay in the data", call, err)
				}
			}
		})
	}
}

// TestAgentEvalTriageObeyedAnswerIsRefused: the model obeys the fixture in a slot that is not card text (an enum, the
// default index, the helper, an extra field) or answers in prose: a permanent PROVIDER_ERROR, charged once, replayed.
func TestAgentEvalTriageObeyedAnswerIsRefused(t *testing.T) {
	for _, fx := range loadEvalFixtures[evalInjectionFixture](t, "injection") {
		q, err := json.Marshal(fx.Untrusted)
		if err != nil {
			t.Fatal(err)
		}
		slots := map[string]string{
			"work_type":   strings.Replace(triageValid, `"essay"`, string(q), 1),
			"feasibility": strings.Replace(triageValid, `"feasibility":"full"`, `"feasibility":`+string(q), 1),
			"default":     strings.Replace(triageValid, `"default":0`, `"default":`+string(q), 1),
			"helper":      strings.Replace(triageValid, `"helper":null`, `"helper":`+string(q), 1),
			"extra field": strings.Replace(triageValid, `"helper":null`, `"helper":null,"action":`+string(q), 1),
			"prose":       fx.Untrusted,
		}
		for slot, out := range slots {
			t.Run(fx.Name+"/"+slot, func(t *testing.T) {
				h := newReaderHub(t)
				h.provider.Push(readerAnswer(t, out))
				for attempt := range 2 {
					res, err := h.triage(t, "tr:a1:1", h.key, 1, fx.Untrusted, "")
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

// TestAgentEvalTriageNeedsAKey: without the tester's key the call is unauthenticated and reaches nothing.
func TestAgentEvalTriageNeedsAKey(t *testing.T) {
	h := newReaderHub(t)
	for _, bearer := range []string{"", "pt_live_obviously-fake"} {
		if _, err := h.triage(t, "tr:a1:1", bearer, 1, "Эссе", ""); connectrpc.CodeOf(err) != connectrpc.CodeUnauthenticated {
			t.Fatalf("bearer %q: err = %v, want unauthenticated", bearer, err)
		}
	}
	if len(h.provider.Calls()) != 0 || len(h.store.Ledger(h.owner)) != 0 {
		t.Fatal("an unauthenticated call reached the provider or the ledger")
	}
}
