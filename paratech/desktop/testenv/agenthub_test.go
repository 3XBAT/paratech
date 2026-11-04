package testenv

import (
	"testing"

	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// The fake Hub takes a leading summary turn from catalog 9 on, followed by a student text, as the real one does.
func TestCheckAgentHistorySummary(t *testing.T) {
	summary := &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_HistorySummary{HistorySummary: "сводка"}}
	text := &hubv1.AgentTurn{Turn: &hubv1.AgentTurn_StudentText{StudentText: "что задали"}}
	tests := []struct {
		name    string
		catalog uint32
		turns   []*hubv1.AgentTurn
		ok      bool
	}{
		{"text only", 9, []*hubv1.AgentTurn{text}, true},
		{"summary then text", 9, []*hubv1.AgentTurn{summary, text}, true},
		{"summary on catalog 8", 8, []*hubv1.AgentTurn{summary, text}, false},
		{"summary alone", 9, []*hubv1.AgentTurn{summary}, false},
		{"none", 9, nil, false},
	}
	for _, tt := range tests {
		req := &hubv1.AgentStepRequest{ThreadId: "th1", CatalogVersion: tt.catalog, Turns: tt.turns}
		if err := checkAgentHistory(req, tt.catalog); (err == nil) != tt.ok {
			t.Errorf("%s: %v, want ok %v", tt.name, err, tt.ok)
		}
	}
}
