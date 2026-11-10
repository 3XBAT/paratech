package actionpolicy

import (
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

func call11(t *testing.T, tool, args string) values.Call {
	t.Helper()
	c, err := values.ParseCallAt("c1", tool, args, values.CatalogWave11)
	if err != nil {
		t.Fatalf("%s %s: %v", tool, args, err)
	}
	return c
}

// TestDecideWave11 is the ADR-0033 §11 table: a value the model typed is a C card, the library tools are U writes.
func TestDecideWave11(t *testing.T) {
	content := Facts{RemovesContent: true}
	tests := []struct {
		name string
		tool string
		args string
		f    Facts
		want Decision
		plan Reason // NeedsPlan of the first write of the request
	}{
		{"set a teacher", "set_teacher", `{"subject":"s1","role":"lecture","name":"Иванов","email":"i@example.invalid"}`, Facts{},
			Decision{Rule: Rule{Class: ClassC}, Card: values.CardTeacher}, ReasonNone},
		{"clear a teacher", "clear_teacher", `{"subject":"s1","role":"lab"}`, Facts{},
			Decision{Rule: Rule{Class: ClassC}, Card: values.CardTeacher}, ReasonNone},
		{"set a profile field", "set_profile_field", `{"field":"group","value":"ИВТ-21"}`, Facts{},
			Decision{Rule: Rule{Class: ClassC}, Card: values.CardProfileField}, ReasonNone},
		{"to the inbox", "move_to_inbox", `{"material":"m1"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}, ReasonNone},
		{"unlink", "unlink_material", `{"material":"m1","lesson":"l1"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}, ReasonNone},
		{"remove a material", "remove_material", `{"material":"m1"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}, ReasonNone},
		{"add an alias", "add_alias", `{"subject":"s1","suggestion":"g-1"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}, ReasonNone},
		{"remove a folder", "remove_folder", `{"subject":"s1","folder":"f1"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}, ReasonNone},
		{"remove an empty subject", "remove_subject", `{"subject":"s1"}`, Facts{}, Decision{Rule: Rule{Class: ClassU}}, ReasonNone},
		{"remove a subject with notes or files", "remove_subject", `{"subject":"s1"}`, content,
			Decision{Rule: Rule{Class: ClassU}}, ReasonRemovesContent},
		{"content of another tool changes nothing", "remove_folder", `{"subject":"s1","folder":"f1"}`, content,
			Decision{Rule: Rule{Class: ClassU}}, ReasonNone},
		{"set a wave-11 setting", "set_setting", `{"setting":"notes_ocr","value":"local"}`, Facts{},
			Decision{Rule: Rule{Class: ClassU}}, ReasonNone},
		{"reply with a chat title", "reply", `{"text":"Готово","chat_title":"Матан"}`, Facts{},
			Decision{Rule: Rule{Class: ClassUI}, Card: values.CardReply}, ReasonNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decide(call11(t, tt.tool, tt.args), tt.f)
			if err != nil || got != tt.want {
				t.Fatalf("Decide = %+v, %v; want %+v", got, err, tt.want)
			}
			if got.Paid || got.AlwaysPlan || got.NoUndo {
				t.Fatalf("wave 11 has no paid, always-planned or undo-less tool: %+v", got)
			}
			if got.Class == ClassC && got.Class.AgentCanExecute() {
				t.Fatal("the agent executes a C tool")
			}
			if r, err := NeedsPlan(values.Tool(tt.tool), tt.f); err != nil || r != tt.plan {
				t.Fatalf("NeedsPlan = %q, %v; want %q", r, err, tt.plan)
			}
			if got.Class != ClassU {
				return
			}
			// A second write of the request needs a plan; removing content keeps its own reason.
			second := tt.f
			second.WritesBefore = 1
			want := ReasonSecondWrite
			if tt.plan == ReasonRemovesContent {
				want = ReasonRemovesContent
			}
			if r, err := NeedsPlan(values.Tool(tt.tool), second); err != nil || r != want {
				t.Fatalf("second write: NeedsPlan = %q, %v; want %q", r, err, want)
			}
		})
	}
}

// TestRulesOfCatalog11: every wave-11 tool has its rule (declared before it is served), C and UI decisions name a
// card, and the policy of the catalog-10 tools is unchanged by the wave.
func TestRulesOfCatalog11(t *testing.T) {
	for _, tool := range values.ToolsAt(values.CatalogWave11) {
		if _, err := Of(tool); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	want := map[values.Tool]Class{
		values.ToolSetTeacher: ClassC, values.ToolClearTeacher: ClassC, values.ToolSetProfileField: ClassC,
		values.ToolMoveToInbox: ClassU, values.ToolUnlinkMaterial: ClassU, values.ToolRemoveMaterial: ClassU,
		values.ToolAddAlias: ClassU, values.ToolRemoveFolder: ClassU, values.ToolRemoveSubject: ClassU,
	}
	for tool, class := range want {
		r, err := Of(tool)
		if err != nil || r.Class != class || r.Paid || r.AlwaysPlan {
			t.Fatalf("%s: %+v, %v", tool, r, err)
		}
		d, err := decide(tool, "", Facts{RemovesContent: true, HasCalendarEvent: true})
		if err != nil || (d.Class == ClassC || d.Class == ClassUI) != (d.Card != "") {
			t.Fatalf("%s: decision %+v, %v", tool, d, err)
		}
		if _, err := values.ParseToolAt(string(tool), values.CatalogWave10); err == nil {
			t.Fatalf("%s is in catalog 10", tool)
		}
	}
}
