package tests

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"paratech/hub/internal/domain/llm/services/toolcatalog"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// cyrillicReply is a reply of about 6000 Cyrillic characters: over 8 KiB of arguments, within the intake of 32 KiB.
var cyrillicReply = strings.Repeat("Преобразование Фурье раскладывает сигнал на частоты. ", 114)

func replyInput(t *testing.T, text string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func panelRequest(key string, version uint32, turns ...*hubv1.AgentTurn) *hubv1.AgentStepRequest {
	req := agStepRequest(key, turns...)
	req.CatalogVersion = version
	return req
}

// TestAgentStepCutsALongReplyFromTheProvider (TASK-223): a returned reply of about 6000 Cyrillic characters passes the
// anthropic adapter, is cut under catalog 7 to at most 4000 characters and 8 KiB, sealed, replayed free and echoed
// into the next step. Under catalog 6 the same answer is the old charged PROVIDER_ERROR.
func TestAgentStepCutsALongReplyFromTheProvider(t *testing.T) {
	input := replyInput(t, cyrillicReply)
	if len(input) <= values.MaxArgumentsBytes || len(input) > values.MaxReturnedArgumentsBytes || utf8.RuneCountInString(cyrillicReply) < 6000 {
		t.Fatalf("fixture: %d bytes, %d characters", len(input), utf8.RuneCountInString(cyrillicReply))
	}
	long := []testenv.FakeToolUse{{ID: "toolu_01", Name: "reply", Input: input}}

	e := newPanelEnv(t, []uint32{6, 7}, long, []testenv.FakeToolUse{{ID: "toolu_02", Name: "reply", Input: `{"text":"Готово."}`}})
	first, err := e.step(t.Context(), panelRequest("ag:1", 7))
	if err != nil {
		t.Fatalf("v7: %v", err)
	}
	calls := first.GetCalls().GetCalls()
	if len(calls) != 1 || calls[0].GetCallId() != "toolu_01" || len(first.GetCalls().GetSeal()) == 0 {
		t.Fatalf("v7 calls = %v", first.GetCalls())
	}
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(calls[0].GetArgumentsJson()), &args); err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(args.Text); n > toolcatalog.MaxReplyTextRunes || len(calls[0].GetArgumentsJson()) > values.MaxArgumentsBytes ||
		!strings.HasSuffix(args.Text, "частоты. …") || !strings.HasPrefix(cyrillicReply, strings.TrimSuffix(args.Text, " …")) {
		t.Fatalf("v7 reply: %d characters, %d bytes", n, len(calls[0].GetArgumentsJson()))
	}
	if used, _ := e.store.Totals(e.owner); used != 960 {
		t.Fatalf("v7: used %d", used)
	}
	if logs := e.logs.String(); !strings.Contains(logs, "reply_cut=true") || strings.Contains(logs, "Фурье") {
		t.Fatalf("v7 log: %s", logs)
	}

	again, err := e.step(t.Context(), panelRequest("ag:1", 7))
	if err != nil || !again.GetReplayed() || again.GetCalls().GetCalls()[0].GetArgumentsJson() != calls[0].GetArgumentsJson() ||
		len(e.api.Requests()) != 1 {
		t.Fatalf("replay = %v, %v", again, err)
	}
	// The echo of the cut call verifies against its seal and finds the kept turn.
	if _, err := e.step(t.Context(), panelRequest("ag:2", 7, echoOf(first)...)); err != nil {
		t.Fatalf("echo of the cut reply: %v (rejected by the API: %d)", err, e.api.Rejected())
	}

	old := newPanelEnv(t, []uint32{6, 7}, long)
	_, err = old.step(t.Context(), panelRequest("ag:1", 6))
	refused(t, err)
	if used, _ := old.store.Totals(old.owner); used != 960 || strings.Contains(old.logs.String(), "reply_cut") {
		t.Fatalf("v6: used %d, log %s", used, old.logs.String())
	}
}

// TestAgentStepRefusesOversizedReturnedCalls: a reply over the intake cap and a call of another tool over 8 KiB stay
// the charged PROVIDER_ERROR under catalog 7: only a reply text is ever cut, and no unbounded size is taken in.
func TestAgentStepRefusesOversizedReturnedCalls(t *testing.T) {
	huge := replyInput(t, strings.Repeat("я", values.MaxReturnedArgumentsBytes/2))
	// Within the schema of cannot_do (10 texts of 300 characters per list), over 8 KiB only in Cyrillic.
	items := make([]string, 0, 10)
	for range 10 {
		items = append(items, strings.Repeat("ш", toolcatalog.MaxTextRunes))
	}
	b, err := json.Marshal(map[string][]string{"your_part": items, "our_part": items})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) <= values.MaxArgumentsBytes || len(b) > values.MaxReturnedArgumentsBytes || len(huge) <= values.MaxReturnedArgumentsBytes {
		t.Fatalf("fixtures: %d and %d bytes", len(b), len(huge))
	}
	for name, use := range map[string]testenv.FakeToolUse{
		"reply over the intake": {ID: "toolu_01", Name: "reply", Input: huge},
		"cannot_do over 8 KiB":  {ID: "toolu_01", Name: "cannot_do", Input: string(b)},
	} {
		e := newPanelEnv(t, []uint32{7}, []testenv.FakeToolUse{use})
		for attempt := range 2 {
			_, err := e.step(t.Context(), panelRequest("ag:1", 7))
			refused(t, err)
			if used, _ := e.store.Totals(e.owner); used != 960 || len(e.api.Requests()) != 1 {
				t.Fatalf("%s attempt %d: used %d, provider requests %d", name, attempt, used, len(e.api.Requests()))
			}
		}
		if strings.Contains(e.logs.String(), "reply_cut") {
			t.Fatalf("%s: log %s", name, e.logs.String())
		}
	}
}
