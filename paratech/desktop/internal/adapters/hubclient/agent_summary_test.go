package hubclient

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	agent "paratech/desktop/internal/domain/agent/values"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// summaryStep is a step request whose window opens with a summary turn, then an earlier pair and the current request.
func summaryStep(t *testing.T) (agent.StepRequest, string) {
	t.Helper()
	const text = "Сводка: обсуждали задачу 5 по физике."
	sum, err := agent.ParseHistorySummary(3, text)
	if err != nil {
		t.Fatal(err)
	}
	req := stepRequest(t)
	req.History = append([]agent.HistoryTurn{sum.Turn(), {StudentText: "что такое горутина"}, {AgentReply: "reply: это"}}, req.History...)
	return req, text
}

// On catalog 9 the summary turn goes first as history_summary, right before a student text, and its text appears
// nowhere else in the request (not in the scope or the mentions).
func TestStepRequestCarriesTheSummaryOnCatalog9(t *testing.T) {
	req, text := summaryStep(t)
	msg, err := agentRequestAt(req, 9)
	if err != nil {
		t.Fatal(err)
	}
	asJSON, err := protojson.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	turns := msg.GetTurns()
	if msg.GetCatalogVersion() != 9 || len(turns) != 6 || turns[0].GetHistorySummary() != text ||
		turns[1].GetStudentText() != "что такое горутина" {
		t.Fatalf("request = %s", asJSON)
	}
	if n := strings.Count(string(asJSON), text); n != 1 || !strings.Contains(string(asJSON), `"historySummary"`) {
		t.Fatalf("the summary text appears %d times: %s", n, asJSON)
	}
	if strings.Contains(protojson.Format(msg.GetScope()), "задачу") || strings.Contains(protojson.Format(msg.GetBase()), "задачу") {
		t.Fatal("the summary text leaked into the scope or the base")
	}
	if via, err := agentRequestOf(req); err != nil || via.GetCatalogVersion() != agent.CatalogVersion ||
		via.GetTurns()[0].GetHistorySummary() != text {
		t.Fatalf("agentRequestOf = %v, %v", via, err)
	}
}

// Below catalog 9 the Hub does not know history_summary: the summary turn is left out and the window opens with the
// student text that followed it; the rest of the request is unchanged.
func TestStepRequestDropsTheSummaryBelowCatalog9(t *testing.T) {
	req, text := summaryStep(t)
	msg, err := agentRequestAt(req, 8)
	if err != nil {
		t.Fatal(err)
	}
	asJSON, err := protojson.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	turns := msg.GetTurns()
	if msg.GetCatalogVersion() != 8 || len(turns) != 5 || turns[0].GetStudentText() != "что такое горутина" ||
		strings.Contains(string(asJSON), text) || strings.Contains(string(asJSON), "historySummary") {
		t.Fatalf("request on catalog 8 = %s", asJSON)
	}
	for _, turn := range turns {
		if _, ok := turn.GetTurn().(*hubv1.AgentTurn_HistorySummary); ok {
			t.Fatalf("a summary turn on catalog 8: %s", asJSON)
		}
	}
	nine, err := agentRequestAt(req, 9)
	if err != nil {
		t.Fatal(err)
	}
	nine.CatalogVersion, nine.Turns = 8, nine.Turns[1:]
	if !proto.Equal(nine, msg) {
		t.Fatalf("catalog 8 differs beyond the summary:\n%v\n%v", nine, msg)
	}
}
