package actionpolicy

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

func call10(t *testing.T, tool, args string) values.Call {
	t.Helper()
	c, err := values.ParseCallAt("c1", tool, args, values.CatalogWave10)
	if err != nil {
		t.Fatalf("%s %s: %v", tool, args, err)
	}
	return c
}

// TestDecideWave10 is the ADR-0033 §10 table, one row per tool and action.
func TestDecideWave10(t *testing.T) {
	event := Facts{HasCalendarEvent: true}
	ids := make([]string, values.MaxMarkAssignments)
	for i := range ids {
		ids[i] = fmt.Sprintf(`"a%d"`, i)
	}
	tests := []struct {
		name string
		tool string
		args string
		f    Facts
		want Decision
	}{
		{"schedule subjects", "propose_subjects_from_schedule", `{}`, Facts{},
			Decision{Rule: Rule{Class: ClassC}, Card: values.CardScheduleSubjects}},
		{"edit", "change_assignment", `{"assignment":"a1","action":"edit","title":"Реферат"}`, Facts{},
			Decision{Rule: Rule{Class: ClassU}}},
		{"edit with an event", "change_assignment", `{"assignment":"a1","action":"edit","due":"2026-11-10"}`, event,
			Decision{Rule: Rule{Class: ClassU}}}, // C-4 stays: the event is updated without asking
		{"restore", "change_assignment", `{"assignment":"a1","action":"restore"}`, event,
			Decision{Rule: Rule{Class: ClassU}}},
		{"delete without an event", "change_assignment", `{"assignment":"a1","action":"delete"}`, Facts{},
			Decision{Rule: Rule{Class: ClassU}}},
		{"delete with an event", "change_assignment", `{"assignment":"a1","action":"delete"}`, event,
			Decision{Rule: Rule{Class: ClassC}, Card: values.CardDeleteAssignment}},
		{"mark one", "mark_assignments_done", `{"assignments":["a1"],"done":true}`, Facts{},
			Decision{Rule: Rule{Class: ClassU}}},
		{"mark fifty: one write", "mark_assignments_done", `{"assignments":[` + strings.Join(ids, ",") + `],"done":false}`, Facts{},
			Decision{Rule: Rule{Class: ClassU}}},
		{"list notes", "list_notes", `{"lesson":"l1"}`, Facts{}, Decision{Rule: Rule{Class: ClassR}}},
		{"add text", "note", `{"action":"add_text","lesson":"l1","text":"x"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}},
		{"add upload", "note", `{"action":"add_upload","note":"n1","upload":"u1"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}},
		{"move note", "note", `{"action":"move","note":"n1","date":"2026-11-03"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}},
		{"rename note", "note", `{"action":"rename","note":"n1","title":"x"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}},
		{"remove note", "note", `{"action":"remove","note":"n1"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}},
		{"get settings", "get_settings", `{}`, Facts{}, Decision{Rule: Rule{Class: ClassR}}},
		{"set setting", "set_setting", `{"setting":"recognition_load","value":"max"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}},
		{"quota", "quota_status", `{}`, Facts{}, Decision{Rule: Rule{Class: ClassR}}},
		{"start recording", "recording", `{"action":"start"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}},
		{"stop recording", "recording", `{"action":"stop"}`, Facts{},
			Decision{Rule: Rule{Class: ClassU}, NoUndo: true}},
		{"offer a button", "offer_button", `{"action":"open_material","material":"m1"}`, Facts{},
			Decision{Rule: Rule{Class: ClassUI}, Card: values.CardOfferButton}},
		{"list assignments by state", "list_assignments", `{"state":"done"}`, Facts{}, Decision{Rule: Rule{Class: ClassR}}},
		{"show a long plan", "show_plan", `{"summary":"План","steps":[{"tool":"list_notes","arguments":{"subject":"s1"}}]}`, Facts{},
			Decision{Rule: Rule{Class: ClassUI}, Card: values.CardPlan}},
		// Facts of another call change nothing elsewhere.
		{"event on a note", "note", `{"action":"remove","note":"n1"}`, event, Decision{Rule: Rule{Class: ClassU}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decide(call10(t, tt.tool, tt.args), tt.f)
			if err != nil || got != tt.want {
				t.Fatalf("Decide = %+v, %v; want %+v", got, err, tt.want)
			}
			if got.Class == ClassU {
				// One write: the first is admitted at once, also for a list of 50 (one journal entry).
				if r, err := NeedsPlan(values.Tool(tt.tool), tt.f); err != nil || r != ReasonNone {
					t.Fatalf("NeedsPlan = %q, %v", r, err)
				}
			}
			if got.Paid {
				t.Fatal("wave 10 has no paid tool")
			}
		})
	}
}

// TestDecideCoversEveryAction: every tool of catalog 10 and every value of its action enum has a decision, the class
// of a tool without actions is its rule, and every C or UI decision names its card.
func TestDecideCoversEveryAction(t *testing.T) {
	for _, tool := range values.ToolsAt(values.CatalogWave10) {
		spec, err := values.SpecAt(tool, values.CatalogWave10)
		if err != nil {
			t.Fatal(err)
		}
		acts := []string{""}
		for _, a := range spec.Args() {
			if a.Name == values.ArgAction {
				acts = a.Values
			}
		}
		_, byAction := actions[tool]
		if byAction != (acts[0] != "") {
			t.Fatalf("%s: action rules %v, action enum %v", tool, byAction, acts)
		}
		if byAction && len(actions[tool]) != len(acts) {
			t.Fatalf("%s: %d action rules for %d actions", tool, len(actions[tool]), len(acts))
		}
		for _, act := range acts {
			for _, f := range []Facts{{}, {HasCalendarEvent: true}} {
				d, err := decide(tool, act, f)
				if err != nil {
					t.Fatalf("%s %q: %v", tool, act, err)
				}
				if (d.Class == ClassC || d.Class == ClassUI) != (d.Card != "") {
					t.Fatalf("%s %q: class %s, card %q", tool, act, d.Class, d.Card)
				}
				if d.NoUndo != (tool == values.ToolRecording && act == values.RecordingStop) {
					t.Fatalf("%s %q: NoUndo %v", tool, act, d.NoUndo)
				}
				r, _ := Of(tool)
				if !byAction && d.Rule != r {
					t.Fatalf("%s: decision %+v, rule %+v", tool, d, r)
				}
			}
		}
	}
}

func TestDecideRefuses(t *testing.T) {
	tests := []struct {
		name string
		tool values.Tool
		act  string
		want error
	}{
		{"unknown tool", "send_email", "", ErrUnknownTool},
		{"unknown assignment action", values.ToolChangeAssignment, "archive", ErrUnknownAction},
		{"assignment without action", values.ToolChangeAssignment, "", ErrUnknownAction},
		{"unknown note action", values.ToolNote, "purge", ErrUnknownAction},
		{"unknown recording action", values.ToolRecording, "pause", ErrUnknownAction},
		{"recording without action", values.ToolRecording, "", ErrUnknownAction},
		{"unknown button", values.ToolOfferButton, "open_url", ErrUnknownAction},
		{"an action of a tool without actions", values.ToolSetSetting, "edit", ErrUnknownAction},
		{"an action case-folded", values.ToolRecording, "STOP", ErrUnknownAction},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, f := range []Facts{{}, {HasCalendarEvent: true}} {
				if d, err := decide(tt.tool, tt.act, f); !errors.Is(err, tt.want) || d != (Decision{}) {
					t.Fatalf("decide = %+v, %v; want %v", d, err, tt.want)
				}
			}
		})
	}
}

// TestDecideCatalog9: the tools of the served catalog decide as their rule (no actions there).
func TestDecideCatalog9(t *testing.T) {
	tests := []struct {
		tool, args string
		want       Decision
	}{
		{"list_subjects", `{}`, Decision{Rule: Rule{Class: ClassR}}},
		{"make_lecture_text", `{"recording":"r1"}`, Decision{Rule: Rule{Class: ClassU, Paid: true, AlwaysPlan: true}}},
		{"propose_send", `{"assignment":"a1","version":"v1"}`, Decision{Rule: Rule{Class: ClassC}, Card: values.CardConfirm}},
		{"reply", `{"text":"Готово"}`, Decision{Rule: Rule{Class: ClassUI}, Card: values.CardReply}},
	}
	for _, tt := range tests {
		c, err := values.ParseCall("c1", tt.tool, tt.args)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := Decide(c, Facts{HasCalendarEvent: true}); err != nil || got != tt.want {
			t.Fatalf("%s: Decide = %+v, %v", tt.tool, got, err)
		}
	}
}
