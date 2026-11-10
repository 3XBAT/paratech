package values

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

var waveTools = []Tool{
	ToolProposeSubjectsFromSchedule, ToolChangeAssignment, ToolMarkAssignmentsDone, ToolListNotes, ToolNote,
	ToolGetSettings, ToolSetSetting, ToolQuotaStatus, ToolRecording, ToolOfferButton,
}

// TestCatalog9SeesNoWaveTool: catalog 10 is served since TASK-326 (CatalogVersion), and ToolsAt(9) still is the v9
// set: exactly the tools of catalog 8, in which nothing of the wave parses or plans. The current catalog is the v10 set.
func TestCatalog9SeesNoWaveTool(t *testing.T) {
	if CatalogVersion != CatalogWave10 || CatalogWave10 != 10 {
		t.Fatalf("CatalogVersion = %d, wave %d", CatalogVersion, CatalogWave10)
	}
	if !slices.Equal(ToolsAt(8), ToolsAt(9)) || len(ToolsAt(9)) != 27 {
		t.Fatalf("catalog 9 = %v, catalog 8 = %v", ToolsAt(9), ToolsAt(8))
	}
	if !slices.Equal(Tools(), ToolsAt(CatalogWave10)) || len(Tools()) != 27+len(waveTools) {
		t.Fatalf("current catalog = %v", Tools())
	}
	for _, tool := range waveTools {
		t.Run(string(tool), func(t *testing.T) {
			if slices.Contains(ToolsAt(9), tool) {
				t.Fatal("catalog 9 lists the tool")
			}
			if _, err := ParseToolAt(string(tool), 9); !errors.Is(err, ErrUnknownTool) {
				t.Fatalf("ParseToolAt(9) = %v", err)
			}
			if _, err := SpecAt(tool, 9); !errors.Is(err, ErrUnknownTool) {
				t.Fatalf("SpecAt(9) = %v", err)
			}
			if _, err := ParseCallAt("c1", string(tool), `{}`, 9); !errors.Is(err, ErrUnknownTool) {
				t.Fatalf("ParseCallAt(9) = %v", err)
			}
			step := `[{"tool":"` + string(tool) + `"}]`
			if _, err := ParseCallAt("c1", "show_plan", `{"summary":"План","steps":`+step+`}`, 9); !errors.Is(err, ErrUnknownTool) {
				t.Fatalf("planned at catalog 9: %v", err)
			}

			// The current catalog serves the tool.
			if !slices.Contains(Tools(), tool) || !tool.Valid() {
				t.Fatal("the current catalog does not list the tool")
			}
			if got, err := ParseTool(string(tool)); err != nil || got != tool {
				t.Fatalf("ParseTool = %v, %v", got, err)
			}
			if _, err := SpecOf(tool); err != nil {
				t.Fatalf("SpecOf = %v", err)
			}
		})
	}
	if _, err := ParseCallAt("c1", "list_assignments", `{"state":"done"}`, 9); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("list_assignments.state at catalog 9 = %v", err)
	}
	if _, err := ParseCall("c1", "list_assignments", `{"state":"done"}`); err != nil {
		t.Fatalf("list_assignments.state at the current catalog = %v", err)
	}
	if spec, _ := SpecAt(ToolListAssignments, 9); len(spec.Args()) != 3 {
		t.Fatalf("list_assignments of catalog 9 = %+v", spec.Args())
	}
}

// TestStoredDataOfCatalog9Restores: a plan stored under catalog 9 decodes under the current catalog unchanged.
func TestStoredDataOfCatalog9Restores(t *testing.T) {
	stored := `[{"tool":"list_assignments","arguments":{"status":"overdue"}},{"tool":"edit_due","arguments":{"assignment":"a1","due":"2026-11-03"}}]`
	steps, err := DecodePlanSteps(stored)
	if err != nil || len(steps) != 2 {
		t.Fatalf("DecodePlanSteps(stored v9 plan) = %+v, %v", steps, err)
	}
	if enc, err := EncodePlanSteps(steps); err != nil || enc != stored {
		t.Fatalf("EncodePlanSteps = %s, %v", enc, err)
	}
}

// TestCatalog10: v10 is v9 (list_assignments with a state) plus the wave.
func TestCatalog10(t *testing.T) {
	want := append(slices.Clone(ToolsAt(9)), waveTools...)
	if got := ToolsAt(CatalogWave10); !slices.Equal(got, want) {
		t.Fatalf("catalog 10 = %v", got)
	}
	if !slices.Equal(ToolsAt(CatalogWave10+1), ToolsAt(CatalogWave10)) {
		t.Fatal("a later catalog is not the newest declared")
	}
	seen := map[Tool]bool{}
	for _, tool := range ToolsAt(CatalogWave10) {
		if seen[tool] {
			t.Fatalf("duplicate %s", tool)
		}
		seen[tool] = true
		if got, err := ParseToolAt(string(tool), CatalogWave10); err != nil || got != tool {
			t.Fatalf("ParseToolAt(%s) = %v, %v", tool, got, err)
		}
		spec, err := SpecAt(tool, CatalogWave10)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range spec.Args() {
			if (a.Type == ArgID || a.Type == ArgIDs || a.Type == ArgCardIDs) != a.Kind.Valid() {
				t.Fatalf("%s.%s: id kind %q", tool, a.Name, a.Kind)
			}
		}
	}
	la, _ := SpecAt(ToolListAssignments, CatalogWave10)
	if args := la.Args(); len(args) != 4 || args[3].Name != "state" || !slices.Equal(args[3].Values, []string{"open", "done", "all"}) {
		t.Fatalf("list_assignments of catalog 10 = %+v", args)
	}
	for _, retiredTool := range RetiredTools() {
		if _, err := ParseToolAt(string(retiredTool), CatalogWave10); !errors.Is(err, ErrUnknownTool) {
			t.Fatalf("%s came back in catalog 10", retiredTool)
		}
	}
	for _, kind := range []IDKind{KindNote, KindScheduleGroup} {
		if !kind.Valid() {
			t.Fatalf("%s is not an id kind", kind)
		}
	}
}

func idList(prefix string, n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf(`"%s%d"`, prefix, i)
	}
	return "[" + strings.Join(items, ",") + "]"
}

func TestWave10Arguments(t *testing.T) {
	text4000 := strings.Repeat("я", MaxReplyTextRunes)
	tests := []struct {
		name string
		tool string
		args string
		ok   bool
	}{
		{"schedule subjects", "propose_subjects_from_schedule", `{}`, true},
		{"schedule subjects with an argument", "propose_subjects_from_schedule", `{"subject":"s1"}`, false},

		{"edit title", "change_assignment", `{"assignment":"a1","action":"edit","title":"Реферат"}`, true},
		{"edit everything", "change_assignment", `{"assignment":"a1","action":"edit","title":"Реферат","note":"до пары","due":"2026-11-10","time":"18:00","subject":"s1"}`, true},
		{"edit nothing", "change_assignment", `{"assignment":"a1","action":"edit"}`, false},
		{"edit a time without its date", "change_assignment", `{"assignment":"a1","action":"edit","time":"18:00"}`, false},
		{"edit with a bad date", "change_assignment", `{"assignment":"a1","action":"edit","due":"2026-02-30"}`, false},
		{"edit with a long title", "change_assignment", `{"assignment":"a1","action":"edit","title":"` + strings.Repeat("я", MaxLabelRunes+1) + `"}`, false},
		{"delete", "change_assignment", `{"assignment":"a1","action":"delete"}`, true},
		{"delete with a title", "change_assignment", `{"assignment":"a1","action":"delete","title":"x"}`, false},
		{"restore", "change_assignment", `{"assignment":"a1","action":"restore"}`, true},
		{"restore with a due", "change_assignment", `{"assignment":"a1","action":"restore","due":"2026-11-10"}`, false},
		{"unknown action", "change_assignment", `{"assignment":"a1","action":"archive"}`, false},
		{"no action", "change_assignment", `{"assignment":"a1","title":"x"}`, false},
		{"no assignment", "change_assignment", `{"action":"delete"}`, false},
		{"unknown field", "change_assignment", `{"assignment":"a1","action":"delete","path":"C:/x"}`, false},

		{"mark one done", "mark_assignments_done", `{"assignments":["a1"],"done":true}`, true},
		{"mark undone", "mark_assignments_done", `{"assignments":["a1"],"done":false}`, true},
		{"mark 50", "mark_assignments_done", `{"assignments":` + idList("a", MaxMarkAssignments) + `,"done":true}`, true},
		{"mark 51", "mark_assignments_done", `{"assignments":` + idList("a", MaxMarkAssignments+1) + `,"done":true}`, false},
		{"mark none", "mark_assignments_done", `{"assignments":[],"done":true}`, false},
		{"mark twice", "mark_assignments_done", `{"assignments":["a1","a1"],"done":true}`, false},
		{"done as a string", "mark_assignments_done", `{"assignments":["a1"],"done":"true"}`, false},
		{"done as a number", "mark_assignments_done", `{"assignments":["a1"],"done":1}`, false},
		{"no done", "mark_assignments_done", `{"assignments":["a1"]}`, false},

		{"notes of a lesson", "list_notes", `{"lesson":"l1"}`, true},
		{"notes of a subject", "list_notes", `{"subject":"s1"}`, true},
		{"notes of both", "list_notes", `{"lesson":"l1","subject":"s1"}`, false},
		{"notes of nothing", "list_notes", `{}`, false},

		{"add text to a lesson", "note", `{"action":"add_text","lesson":"l1","text":"Формула Байеса"}`, true},
		{"add 4000 characters", "note", `{"action":"add_text","note":"n1","text":"` + text4000 + `"}`, true},
		{"add 4001 characters", "note", `{"action":"add_text","note":"n1","text":"` + text4000 + `я"}`, false},
		{"add text with angle brackets and lines", "note", `{"action":"add_text","date":"2026-11-03","text":"a < b\nb > c","title":"Мои заметки"}`, true},
		{"add text with a control character", "note", `{"action":"add_text","note":"n1","text":"a\u0007b"}`, false},
		{"add text without text", "note", `{"action":"add_text","note":"n1"}`, false},
		{"add blank text", "note", `{"action":"add_text","note":"n1","text":"   "}`, false},
		{"add text to two places", "note", `{"action":"add_text","note":"n1","lesson":"l1","text":"x"}`, false},
		{"add text nowhere", "note", `{"action":"add_text","text":"x"}`, false},
		{"add text with an upload", "note", `{"action":"add_text","note":"n1","text":"x","upload":"u1"}`, false},
		{"add upload", "note", `{"action":"add_upload","lesson":"l1","upload":"u1"}`, true},
		{"add upload without upload", "note", `{"action":"add_upload","lesson":"l1"}`, false},
		{"add upload with text", "note", `{"action":"add_upload","lesson":"l1","upload":"u1","text":"x"}`, false},
		{"move to a day", "note", `{"action":"move","note":"n1","date":"2026-11-03"}`, true},
		{"move to a lesson", "note", `{"action":"move","note":"n1","lesson":"l1"}`, true},
		{"move nowhere", "note", `{"action":"move","note":"n1"}`, false},
		{"move to both", "note", `{"action":"move","note":"n1","lesson":"l1","date":"2026-11-03"}`, false},
		{"move with a title", "note", `{"action":"move","note":"n1","lesson":"l1","title":"x"}`, false},
		{"rename", "note", `{"action":"rename","note":"n1","title":"Лекция 3"}`, true},
		{"rename without title", "note", `{"action":"rename","note":"n1"}`, false},
		{"rename on two lines", "note", `{"action":"rename","note":"n1","title":"a\nb"}`, false},
		{"remove", "note", `{"action":"remove","note":"n1"}`, true},
		{"remove without note", "note", `{"action":"remove","lesson":"l1"}`, false},
		{"remove with text", "note", `{"action":"remove","note":"n1","text":"x"}`, false},
		{"unknown note action", "note", `{"action":"purge","note":"n1"}`, false},
		{"note id of another shape", "note", `{"action":"remove","note":"../n1"}`, false},

		{"settings", "get_settings", `{}`, true},
		{"settings with an argument", "get_settings", `{"setting":"ac_power_only"}`, false},
		{"quota", "quota_status", `{}`, true},
		{"quota with an argument", "quota_status", `{"reset":"x"}`, false},

		{"prompts off", "set_setting", `{"setting":"recording_prompts","value":"off"}`, true},
		{"ac power on", "set_setting", `{"setting":"ac_power_only","value":"on"}`, true},
		{"free work notice is gone (TASK-342)", "set_setting", `{"setting":"free_work_notice","value":"off"}`, false},
		{"load max", "set_setting", `{"setting":"recognition_load","value":"max"}`, true},
		{"load gentle", "set_setting", `{"setting":"recognition_load","value":"gentle"}`, true},
		{"load on", "set_setting", `{"setting":"recognition_load","value":"on"}`, false},
		{"toggle max", "set_setting", `{"setting":"ac_power_only","value":"max"}`, false},
		{"unknown setting", "set_setting", `{"setting":"hub_url","value":"on"}`, false},
		{"unknown value", "set_setting", `{"setting":"ac_power_only","value":"yes"}`, false},
		{"bool value", "set_setting", `{"setting":"ac_power_only","value":true}`, false},
		{"no value", "set_setting", `{"setting":"ac_power_only"}`, false},

		{"start recording", "recording", `{"action":"start"}`, true},
		{"stop recording", "recording", `{"action":"stop"}`, true},
		{"pause recording", "recording", `{"action":"pause"}`, false},
		{"recording of a lesson", "recording", `{"action":"start","lesson":"l1"}`, false},

		{"open material", "offer_button", `{"action":"open_material","material":"m1"}`, true},
		{"open material without it", "offer_button", `{"action":"open_material"}`, false},
		{"open material by a lesson", "offer_button", `{"action":"open_material","lesson":"l1"}`, false},
		{"open material and a lesson", "offer_button", `{"action":"open_material","material":"m1","lesson":"l1"}`, false},
		{"call link", "offer_button", `{"action":"open_call_link","lesson":"l1"}`, true},
		{"summary", "offer_button", `{"action":"open_summary","recording":"r1"}`, true},
		{"save as", "offer_button", `{"action":"save_as","work":"w1"}`, true},
		{"sign in", "offer_button", `{"action":"google_sign_in"}`, true},
		{"sign in with an id", "offer_button", `{"action":"google_sign_in","work":"w1"}`, false},
		{"button with a url", "offer_button", `{"action":"open_help","url":"https://example.invalid"}`, false},
		{"button with a label", "offer_button", `{"action":"open_help","label":"Жми"}`, false},
		{"unknown button", "offer_button", `{"action":"delete_everything"}`, false},

		{"assignments by state", "list_assignments", `{"state":"done"}`, true},
		{"assignments of every state", "list_assignments", `{"state":"all","status":"overdue"}`, true},
		{"assignments of an unknown state", "list_assignments", `{"state":"archived"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCallAt("c1", tt.tool, tt.args, CatalogWave10)
			if tt.ok != (err == nil) {
				t.Fatalf("ParseCallAt = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidArguments) {
				t.Fatalf("error = %v, want ErrInvalidArguments", err)
			}
		})
	}
}

func TestWave10ArgumentValues(t *testing.T) {
	c, err := ParseCallAt("c1", "mark_assignments_done", `{"assignments":["a2","a1"],"done":false}`, CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	if done, ok := c.Args().Bool("done"); !ok || done {
		t.Fatalf("done = %v, %v", done, ok)
	}
	if ids := c.Args().IDs("assignments"); len(ids) != 2 || ids[0].ID() != "a2" || len(c.Refs()) != 2 {
		t.Fatalf("assignments = %v", ids)
	}
	if _, ok := c.Args().Enum("done"); ok {
		t.Fatal("a bool reads as an enum")
	}

	n, err := ParseCallAt("c1", "note", `{"action":"add_text","note":"n1","text":"  Формула  "}`, CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := n.Args().Text("text"); !ok || text != "Формула" {
		t.Fatalf("text = %q (trimmed as a reply text)", text)
	}
	if refs := n.Refs(); len(refs) != 1 || refs[0].Kind() != KindNote {
		t.Fatalf("note refs = %v", refs)
	}
	if act, _ := n.Args().Enum(ArgAction); act != NoteAddText {
		t.Fatalf("action = %q", act)
	}

	b, err := ParseCallAt("c1", "offer_button", `{"action":"open_transcript","recording":"r1"}`, CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	if refs := b.Refs(); len(refs) != 1 || refs[0].Kind() != KindRecording {
		t.Fatalf("button refs = %v (the id goes through the provenance check)", refs)
	}
}

func TestSettingValues(t *testing.T) {
	want := map[Setting][]string{
		SettingRecordingPrompts: {"on", "off"},
		SettingACPowerOnly:      {"on", "off"},
		SettingRecognitionLoad:  {"gentle", "normal", "max"},
	}
	if len(Settings()) != len(want) {
		t.Fatalf("settings = %v", Settings())
	}
	for _, s := range Settings() {
		if !s.Valid() || !slices.Equal(SettingValues(s), want[s]) {
			t.Fatalf("%s values = %v", s, SettingValues(s))
		}
	}
	for _, unknown := range []Setting{"hub_url", "free_work_notice"} {
		if unknown.Valid() || SettingValues(unknown) != nil || ValidSettingValue(unknown, "on") {
			t.Fatalf("an unknown setting %s has values", unknown)
		}
	}
	spec, _ := SpecAt(ToolSetSetting, CatalogWave10)
	if args := spec.Args(); !slices.Equal(args[1].Values, []string{"on", "off", "gentle", "normal", "max"}) {
		t.Fatalf("value enum = %v", args[1].Values)
	}
}

func TestOfferButtons(t *testing.T) {
	want := []OfferAction{
		"open_material", "open_call_link", "copy_call_link", "open_summary", "open_lecture_text", "open_transcript",
		"save_as", "google_sign_in", "google_sync", "download_model", "sound_check", "open_help", "undo_action",
		"start_recording",
	}
	if !slices.Equal(OfferActions(), want) {
		t.Fatalf("buttons = %v", OfferActions())
	}
	ids := map[string]IDKind{"material": KindMaterial, "lesson": KindLesson, "recording": KindRecording, "work": KindWork}
	for _, a := range OfferActions() {
		arg, kind, ok := OfferTarget(a)
		if !ok || (arg == "") != (kind == "") || arg != "" && ids[arg] != kind {
			t.Fatalf("%s target = %q %q %v", a, arg, kind, ok)
		}
		args := `{"action":"` + string(a) + `"}`
		if arg != "" {
			args = `{"action":"` + string(a) + `","` + arg + `":"x1"}`
		}
		if _, err := ParseCallAt("c1", "offer_button", args, CatalogWave10); err != nil {
			t.Fatalf("%s: %v", a, err)
		}
		// Every other ID is refused for this button.
		for other := range ids {
			if other == arg {
				continue
			}
			extra := strings.TrimSuffix(args, "}") + `,"` + other + `":"x2"}`
			if _, err := ParseCallAt("c1", "offer_button", extra, CatalogWave10); !errors.Is(err, ErrInvalidArguments) {
				t.Fatalf("%s took %s: %v", a, other, err)
			}
		}
	}
	if _, _, ok := OfferTarget("open_url"); ok {
		t.Fatal("an unknown button has a target")
	}
}

func TestWave10PlanSteps(t *testing.T) {
	steps := `[` +
		`{"tool":"create_assignment","arguments":{"title":"Реферат"}},` +
		`{"tool":"change_assignment","arguments":{"action":"edit","due":"2026-11-10"}},` +
		`{"tool":"mark_assignments_done","arguments":{"assignments":["a1","a2"],"done":true}},` +
		`{"tool":"note","arguments":{"action":"add_text","lesson":"l1"}},` +
		`{"tool":"set_setting","arguments":{"setting":"recognition_load","value":"normal"}},` +
		`{"tool":"recording","arguments":{"action":"stop"}}` +
		`]`
	c, err := ParseCallAt("c1", "show_plan", `{"summary":"План","steps":`+steps+`}`, CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Args().PlanSteps("steps")
	if len(got) != 6 || !slices.Equal(got[1].Deferred(), []string{"assignment"}) {
		t.Fatalf("steps = %+v", got)
	}

	// The call fills the deferred assignment; the note's text is free, everything else is as planned.
	edit, _ := ParseCallAt("c2", "change_assignment", `{"assignment":"a9","action":"edit","due":"2026-11-10"}`, CatalogWave10)
	other, _ := ParseCallAt("c3", "change_assignment", `{"assignment":"a9","action":"delete"}`, CatalogWave10)
	if !got[1].Matches(edit) || got[1].Matches(other) {
		t.Fatal("an edit step matched wrongly")
	}
	add, _ := ParseCallAt("c4", "note", `{"action":"add_text","lesson":"l1","text":"Любой текст"}`, CatalogWave10)
	moved, _ := ParseCallAt("c5", "note", `{"action":"add_text","lesson":"l2","text":"Любой текст"}`, CatalogWave10)
	if !got[3].Matches(add) || got[3].Matches(moved) {
		t.Fatal("a note step matched wrongly")
	}
	undone, _ := ParseCallAt("c6", "mark_assignments_done", `{"assignments":["a2","a1"],"done":false}`, CatalogWave10)
	done, _ := ParseCallAt("c7", "mark_assignments_done", `{"assignments":["a2","a1"],"done":true}`, CatalogWave10)
	if got[2].Matches(undone) || !got[2].Matches(done) {
		t.Fatal("a mark step matched wrongly")
	}

	enc, err := EncodePlanSteps(got)
	if err != nil || !strings.Contains(enc, `"done":true`) {
		t.Fatalf("EncodePlanSteps = %s, %v", enc, err)
	}

	bad := []string{
		`[{"tool":"note","arguments":{"action":"add_text","lesson":"l1","text":"x"}}]`,                       // free text is not planned
		`[{"tool":"note","arguments":{"action":"remove"}}]`,                                                  // no note
		`[{"tool":"change_assignment","arguments":{"assignment":"a1","action":"delete","title":"x"}}]`,       // delete takes no title
		`[{"tool":"set_setting","arguments":{"setting":"ac_power_only","value":"max"}}]`,                     // another setting's value
		`[{"tool":"offer_button","arguments":{"action":"open_material"}}]`,                                   // no material
		`[{"tool":"mark_assignments_done","arguments":{"assignments":["a1"]}}]`,                              // no done
		`[{"tool":"change_assignment","arguments":{"action":"edit","title":"x"}}]`,                           // nothing creates the assignment
		`[{"tool":"list_notes","arguments":{"lesson":"l1","subject":"s1"}}]`,                                 // one of
		`[{"tool":"mark_assignments_done","arguments":{"assignments":` + idList("a", 51) + `,"done":true}}]`, // 51 ids
	}
	for _, b := range bad {
		if _, err := ParseCallAt("c1", "show_plan", `{"summary":"План","steps":`+b+`}`, CatalogWave10); !errors.Is(err, ErrInvalidArguments) {
			t.Errorf("plan %s = %v", b, err)
		}
	}
}

func TestWave10Cards(t *testing.T) {
	p1 := mustRef(t, KindProposal, "p1")
	g1, g2 := mustRef(t, KindScheduleGroup, "g1"), mustRef(t, KindScheduleGroup, "g2")
	a1, m1 := mustRef(t, KindAssignment, "a1"), mustRef(t, KindMaterial, "m1")
	many := make([]Ref, MaxScheduleGroups+1)
	for i := range many {
		many[i] = mustRef(t, KindScheduleGroup, fmt.Sprintf("g%d", i))
	}
	tests := []struct {
		name string
		card Card
		kind CardKind
		ok   bool
	}{
		{"schedule", ScheduleSubjectsCard{Proposal: p1, Groups: []Ref{g1, g2}}, CardScheduleSubjects, true},
		{"schedule at the limit", ScheduleSubjectsCard{Proposal: p1, Groups: many[:MaxScheduleGroups]}, CardScheduleSubjects, true},
		{"schedule over the limit", ScheduleSubjectsCard{Proposal: p1, Groups: many}, CardScheduleSubjects, false},
		{"schedule without groups", ScheduleSubjectsCard{Proposal: p1}, CardScheduleSubjects, false},
		{"schedule without proposal", ScheduleSubjectsCard{Groups: []Ref{g1}}, CardScheduleSubjects, false},
		{"schedule twice a group", ScheduleSubjectsCard{Proposal: p1, Groups: []Ref{g1, g1}}, CardScheduleSubjects, false},
		{"schedule with a subject", ScheduleSubjectsCard{Proposal: p1, Groups: []Ref{mustRef(t, KindSubject, "s1")}}, CardScheduleSubjects, false},
		{"delete", DeleteAssignmentCard{Assignment: a1}, CardDeleteAssignment, true},
		{"delete a material", DeleteAssignmentCard{Assignment: m1}, CardDeleteAssignment, false},
		{"delete nothing", DeleteAssignmentCard{}, CardDeleteAssignment, false},
		{"button", OfferButtonCard{Action: OfferOpenMaterial, Target: m1}, CardOfferButton, true},
		{"button without id", OfferButtonCard{Action: OfferGoogleSync}, CardOfferButton, true},
		{"button missing its id", OfferButtonCard{Action: OfferOpenMaterial}, CardOfferButton, false},
		{"button with a wrong id", OfferButtonCard{Action: OfferOpenMaterial, Target: a1}, CardOfferButton, false},
		{"button with an extra id", OfferButtonCard{Action: OfferGoogleSync, Target: m1}, CardOfferButton, false},
		{"unknown button", OfferButtonCard{Action: "open_url"}, CardOfferButton, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.card.Kind() != tt.kind {
				t.Fatalf("kind = %s", tt.card.Kind())
			}
			err := tt.card.Validate()
			if tt.ok != (err == nil) || err != nil && !errors.Is(err, ErrInvalidCard) {
				t.Fatalf("Validate = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}
