package actionpolicy

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

func TestEveryToolHasARule(t *testing.T) {
	tools := append(values.Tools(), values.RetiredTools()...)
	for _, tool := range tools {
		r, err := Of(tool)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if r.AlwaysPlan && (r.Class != ClassU || !r.Paid) {
			t.Fatalf("%s: AlwaysPlan only for paid U tools", tool)
		}
	}
	if len(rules) != len(tools) {
		t.Fatalf("rules has %d entries, catalog and retired tools %d", len(rules), len(tools))
	}
	// A retired tool keeps its rule: the journal restores its stored actions (catalog v4 retired generate_draft).
	if r, err := Of(values.ToolGenerateDraft); err != nil || r.Class != ClassU || !r.Paid {
		t.Fatalf("Of(generate_draft) = %+v, %v", r, err)
	}
	if _, err := Of("send_email"); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("Of(unknown) = %v", err)
	}
}

func TestClassProperties(t *testing.T) {
	tests := []struct {
		class   Class
		execute bool
		write   bool
	}{
		{ClassR, true, false},
		{ClassU, true, true},
		{ClassC, false, false},
		{ClassUI, true, false},
	}
	for _, tt := range tests {
		if tt.class.AgentCanExecute() != tt.execute || tt.class.IsWrite() != tt.write {
			t.Fatalf("%s: execute=%v write=%v", tt.class, tt.class.AgentCanExecute(), tt.class.IsWrite())
		}
	}
}

func TestNeedsPlan(t *testing.T) {
	tests := []struct {
		name string
		tool values.Tool
		f    Facts
		want Reason
	}{
		{"first free write", values.ToolCreateSubject, Facts{}, ReasonNone},
		{"second write", values.ToolCreateFolder, Facts{WritesBefore: 1}, ReasonSecondWrite},
		{"cheap paid job", values.ToolMakeSummary, Facts{CostMicroUSD: 20_000}, ReasonNone},
		{"paid job at the threshold", values.ToolFindHomework, Facts{CostMicroUSD: PlanThresholdMicroUSD}, ReasonNone},
		{"paid job above the threshold", values.ToolGenerateDraft, Facts{CostMicroUSD: PlanThresholdMicroUSD + 1}, ReasonCostly},
		{"paid job with unknown cost", values.ToolTriageAssignment, Facts{}, ReasonCostly},
		{"lecture text always", values.ToolMakeLectureText, Facts{CostMicroUSD: 1}, ReasonAlwaysPlan},
		{"turnkey writing always", values.ToolWriteWork, Facts{CostMicroUSD: 1}, ReasonAlwaysPlan},
		{"triage partly", values.ToolEditDue, Facts{Triage: TriagePartly}, ReasonTriage},
		{"triage impossible", values.ToolEditDue, Facts{Triage: TriageImpossible}, ReasonTriage},
		{"triage full", values.ToolEditDue, Facts{Triage: TriageFull}, ReasonNone},
		{"replaces a ready summary", values.ToolMakeSummary, Facts{CostMicroUSD: 1, ReplacesReady: true}, ReasonReplaces},
		{"replaces a ready lecture text", values.ToolMakeLectureText, Facts{CostMicroUSD: 1, ReplacesReady: true}, ReasonAlwaysPlan},
		{"read-only never", values.ToolAnswerQuestion, Facts{WritesBefore: 3}, ReasonNone},
		{"C never", values.ToolProposeSend, Facts{WritesBefore: 3}, ReasonNone},
		{"UI never", values.ToolShowPlan, Facts{WritesBefore: 3}, ReasonNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NeedsPlan(tt.tool, tt.f)
			if err != nil || got != tt.want {
				t.Fatalf("NeedsPlan = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	if _, err := NeedsPlan("x", Facts{}); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("NeedsPlan(unknown) = %v", err)
	}
}

// TestPaidWriteWithoutPlan pins the money rule over every tool (TASK-170): a paid write of unknown cost never runs
// without a plan, and no write after the first one of a request does, whatever the cost estimate or triage say.
func TestPaidWriteWithoutPlan(t *testing.T) {
	costs := []int64{-1, 0, 1, PlanThresholdMicroUSD}
	triages := []Triage{"", TriageFull}
	for tool, r := range rules {
		if r.Class != ClassU {
			continue
		}
		for _, cost := range costs {
			for _, tr := range triages {
				first, err := NeedsPlan(tool, Facts{CostMicroUSD: cost, Triage: tr})
				if err != nil {
					t.Fatal(err)
				}
				if r.Paid && cost <= 0 && first == ReasonNone {
					t.Errorf("%s: paid write of unknown cost (%d) runs without a plan", tool, cost)
				}
				second, err := NeedsPlan(tool, Facts{WritesBefore: 1, CostMicroUSD: cost, Triage: tr})
				if err != nil {
					t.Fatal(err)
				}
				if second == ReasonNone {
					t.Errorf("%s: second write (cost %d, triage %q) runs without a plan", tool, cost, tr)
				}
			}
		}
	}
}

func TestCheckProvenance(t *testing.T) {
	ref := func(k values.IDKind, id string) values.Ref {
		r, err := values.NewRef(k, id)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	known := values.NewRefSet(ref(values.KindLesson, "x1"), ref(values.KindSubject, "s1"))
	tests := []struct {
		name string
		refs []values.Ref
		ok   bool
	}{
		{"none", nil, true},
		{"known", []values.Ref{ref(values.KindLesson, "x1"), ref(values.KindSubject, "s1")}, true},
		{"unknown id", []values.Ref{ref(values.KindSubject, "s2")}, false},
		{"same id other kind", []values.Ref{ref(values.KindRecording, "x1")}, false},
		{"one unknown among known", []values.Ref{ref(values.KindSubject, "s1"), ref(values.KindUpload, "u9")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckProvenance(tt.refs, known)
			if tt.ok != (err == nil) {
				t.Fatalf("CheckProvenance error = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrUnknownID) {
				t.Fatalf("error %v is not ErrUnknownID", err)
			}
		})
	}
}
