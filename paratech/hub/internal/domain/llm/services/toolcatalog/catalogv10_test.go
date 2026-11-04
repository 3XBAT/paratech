package toolcatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"paratech/hub/internal/domain/llm/values"
)

const (
	goldenV9  = "../../../../../../shared/testdata/tool_catalog_v9.json"
	goldenV10 = "../../../../../../shared/testdata/tool_catalog_v10.json"
)

// planOf is a show_plan call of n steps, each the given step object.
func planOf(t *testing.T, n int, step map[string]any) string {
	t.Helper()
	steps := make([]map[string]any, n)
	for i := range steps {
		steps[i] = step
	}
	b, err := json.Marshal(map[string]any{"summary": "План", "steps": steps})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// withoutDescriptions is a JSON Schema with every "description" removed, re-encoded with sorted keys.
func withoutDescriptions(t *testing.T, schema string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(schema), &v); err != nil {
		t.Fatal(err)
	}
	var strip func(any)
	strip = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			delete(x, "description")
			for _, e := range x {
				strip(e)
			}
		case []any:
			for _, e := range x {
				strip(e)
			}
		}
	}
	strip(v)
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestCatalogV10 (TASK-325, ADR-0033 §10): v10 keeps the description and the schema shape of every v9 tool but
// list_assignments (state) and show_plan (200 steps, typed step arguments); only the shorter type texts differ. It adds
// the wave-10 tools with the classes and schemas of their types.
func TestCatalogV10(t *testing.T) {
	v9, v10 := parseFile(t, goldenV9), parseFile(t, goldenV10)
	if v10.Version() != 10 || len(v10.Specs()) != len(v9.Specs())+10 {
		t.Fatalf("v10: version %d, %d tools", v10.Version(), len(v10.Specs()))
	}
	changed := map[string]bool{"list_assignments": true, "show_plan": true}
	for _, old := range v9.Specs() {
		got := specFor(t, v10, old.Name())
		if got.Description() != old.Description() ||
			(!changed[old.Name()] && withoutDescriptions(t, got.Schema()) != withoutDescriptions(t, old.Schema())) {
			t.Errorf("%s changed:\nv9  %s\nv10 %s", old.Name(), old.Schema(), got.Schema())
		}
	}

	var plan struct {
		Properties map[string]struct {
			MaxItems int `json:"maxItems"`
			Items    struct {
				Properties map[string]struct {
					AdditionalProperties map[string]any `json:"additionalProperties"`
				} `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	for _, tc := range []struct {
		c        *Catalog
		maxSteps int
		anyOf    bool
	}{{v9, maxPlanSteps, false}, {v10, maxPlanSteps10, true}} {
		if err := json.Unmarshal([]byte(specFor(t, tc.c, "show_plan").Schema()), &plan); err != nil {
			t.Fatal(err)
		}
		steps := plan.Properties["steps"]
		_, anyOf := steps.Items.Properties["arguments"].AdditionalProperties["anyOf"]
		if steps.MaxItems != tc.maxSteps || anyOf != tc.anyOf {
			t.Errorf("v%d show_plan steps: maxItems %d, anyOf %v", tc.c.Version(), steps.MaxItems, anyOf)
		}
	}

	schema := func(tool string) string { return specFor(t, v10, tool).Schema() }
	for tool, parts := range map[string][]string{
		"mark_assignments_done": {`"assignments":{`, `"maxItems":50`, `"done":{"description":"true or false","type":"boolean"}`},
		"list_assignments":      {`"state":{"description":"one of values","enum":["open","done","all"],"type":"string"}`},
		"note":                  {`"text":{"description":"plain text without HTML or Markdown, at most 4000 characters`, `"maxLength":4000`},
		"offer_button":          {`"enum":["open_material",`, `"work":{"description":"opaque desktop ID`},
		"job_status":            {`"maxItems":20`},
		// A step string is bounded by the longest type a step takes (query); the check holds each to its own type.
		"show_plan": {`"additionalProperties":{"anyOf":[{"maxLength":500,"minLength":1,"type":"string"},{"type":"boolean"},`},
	} {
		for _, p := range parts {
			if !strings.Contains(schema(tool), p) {
				t.Errorf("%s schema %s lacks %s", tool, schema(tool), p)
			}
		}
	}
	for tool, class := range map[string]string{
		"propose_subjects_from_schedule": "confirmation card", "offer_button": "Card shown", "note": "Reversible",
		"list_notes": "Read-only", "recording": "Reversible",
	} {
		if d := specFor(t, v10, tool).Description(); !strings.Contains(d, class) {
			t.Errorf("%s description %q", tool, d)
		}
	}
	if d := specFor(t, v10, "list_notes").Description(); !strings.Contains(d, "Give exactly one of: lesson, subject.") {
		t.Errorf("list_notes description %q", d)
	}
}

// TestCatalogV10Calls: the calls of the wave pass with the types of their arguments and fail without them. The Hub
// checks the schema only; the rules that tie arguments to an action are the desktop's (is_error, values/wave10.go).
func TestCatalogV10Calls(t *testing.T) {
	v10 := parseFile(t, goldenV10)
	ids := func(n int) string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf(`"asg-%d"`, i)
		}
		return "[" + strings.Join(out, ",") + "]"
	}
	note := func(text string) string {
		b, err := json.Marshal(map[string]any{"action": "add_text", "date": "2026-11-04", "text": text})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	for name, c := range map[string][2]string{
		"schedule subjects":   {"propose_subjects_from_schedule", `{}`},
		"edit":                {"change_assignment", `{"assignment":"asg-1","action":"edit","due":"2026-11-10","time":"09:00"}`},
		"delete":              {"change_assignment", `{"assignment":"asg-1","action":"delete"}`},
		"mark 50":             {"mark_assignments_done", `{"assignments":` + ids(50) + `,"done":true}`},
		"undo done":           {"mark_assignments_done", `{"assignments":["asg-1"],"done":false}`},
		"done list":           {"list_assignments", `{"state":"done"}`},
		"notes of a lesson":   {"list_notes", `{"lesson":"les-1"}`},
		"note 4000":           {"note", note(strings.Repeat("я", MaxReplyTextRunes))},
		"note paragraphs":     {"note", note("Первое.\n\nВторое: a < b.")},
		"rename":              {"note", `{"action":"rename","note":"note-1","title":"Пределы"}`},
		"settings":            {"get_settings", `{}`},
		"setting":             {"set_setting", `{"setting":"recognition_load","value":"max"}`},
		"quota":               {"quota_status", `{}`},
		"stop":                {"recording", `{"action":"stop"}`},
		"open material":       {"offer_button", `{"action":"open_material","material":"mat-1"}`},
		"sign in":             {"offer_button", `{"action":"google_sign_in"}`},
		"plan of 200":         {"show_plan", planOf(t, maxPlanSteps10, map[string]any{"tool": "list_subjects"})},
		"planned mark":        {"show_plan", planOf(t, 1, map[string]any{"tool": "mark_assignments_done", "arguments": map[string]any{"assignments": []string{"asg-1"}, "done": true}})},
		"planned change note": {"show_plan", planOf(t, 1, map[string]any{"tool": "change_assignment", "arguments": map[string]any{"assignment": "asg-1", "action": "edit", "note": strings.Repeat("я", 200)}})},
		"planned query 500":   {"show_plan", planOf(t, 1, map[string]any{"tool": "search", "arguments": map[string]any{"query": strings.Repeat("я", MaxQueryRunes)}})},
		"planned setting":     {"show_plan", planOf(t, 1, map[string]any{"tool": "set_setting", "arguments": map[string]any{"setting": "ac_power_only", "value": "on"}})},
		"planned note move":   {"show_plan", planOf(t, 1, map[string]any{"tool": "note", "arguments": map[string]any{"action": "move", "note": "note-1", "date": "2026-11-05"}})},
	} {
		if err := v10.Check(call(t, c[0], c[1])); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	for name, c := range map[string][2]string{
		"mark 51":               {"mark_assignments_done", `{"assignments":` + ids(51) + `,"done":true}`},
		"mark none":             {"mark_assignments_done", `{"assignments":[],"done":true}`},
		"done as a string":      {"mark_assignments_done", `{"assignments":["asg-1"],"done":"true"}`},
		"done missing":          {"mark_assignments_done", `{"assignments":["asg-1"]}`},
		"unknown action":        {"change_assignment", `{"assignment":"asg-1","action":"archive"}`},
		"unknown state":         {"list_assignments", `{"state":"closed"}`},
		"notes of both":         {"list_notes", `{"lesson":"les-1","subject":"s1"}`},
		"notes of nothing":      {"list_notes", `{}`},
		"note 4001":             {"note", note(strings.Repeat("я", MaxReplyTextRunes+1))},
		"note blank":            {"note", note(" \n ")},
		"note control":          {"note", note("a\x1bb")},
		"unknown setting":       {"set_setting", `{"setting":"free_work_notice","value":"on"}`},
		"setting free value":    {"set_setting", `{"setting":"ac_power_only","value":"yes"}`},
		"recording pause":       {"recording", `{"action":"pause"}`},
		"button with a url":     {"offer_button", `{"action":"open_material","url":"https://example.com/x"}`},
		"button url as action":  {"offer_button", `{"action":"https://example.com/x"}`},
		"button with a label":   {"offer_button", `{"action":"open_help","label":"Открыть"}`},
		"button id with spaces": {"offer_button", `{"action":"open_material","material":"rm -rf /"}`},
		"plan of 201":           {"show_plan", planOf(t, maxPlanSteps10+1, map[string]any{"tool": "list_subjects"})},
		"planned button":        {"show_plan", planOf(t, 1, map[string]any{"tool": "offer_button", "arguments": map[string]any{"action": "open_help"}})},
		"planned done string":   {"show_plan", planOf(t, 1, map[string]any{"tool": "mark_assignments_done", "arguments": map[string]any{"assignments": []string{"asg-1"}, "done": "yes"}})},
		"planned ids as string": {"show_plan", planOf(t, 1, map[string]any{"tool": "mark_assignments_done", "arguments": map[string]any{"assignments": "asg-1", "done": true}})},
		"planned label array":   {"show_plan", planOf(t, 1, map[string]any{"tool": "create_subject", "arguments": map[string]any{"name": []string{"a"}}})},
		// The desktop refuses the text of note in a step, so the Hub does too; enums keep their values.
		"planned note text":      {"show_plan", planOf(t, 1, map[string]any{"tool": "note", "arguments": map[string]any{"action": "add_text", "date": "2026-11-04", "text": "Пределы"}})},
		"planned unknown action": {"show_plan", planOf(t, 1, map[string]any{"tool": "change_assignment", "arguments": map[string]any{"assignment": "asg-1", "action": "archive"}})},
		"planned free value":     {"show_plan", planOf(t, 1, map[string]any{"tool": "set_setting", "arguments": map[string]any{"setting": "ac_power_only", "value": "yes"}})},
		"planned enum as bool":   {"show_plan", planOf(t, 1, map[string]any{"tool": "recording", "arguments": map[string]any{"action": true}})},
		"planned query 501":      {"show_plan", planOf(t, 1, map[string]any{"tool": "search", "arguments": map[string]any{"query": strings.Repeat("я", MaxQueryRunes+1)}})},
		"planned label 81":       {"show_plan", planOf(t, 1, map[string]any{"tool": "create_subject", "arguments": map[string]any{"name": strings.Repeat("я", MaxLabelRunes+1)}})},
	} {
		if err := v10.Check(call(t, c[0], c[1])); !errors.Is(err, ErrInvalidCall) {
			t.Errorf("%s: err = %v, want ErrInvalidCall", name, err)
		}
	}
}

// TestCatalogV10OlderCatalogsUnchanged: catalog 9 keeps plans of 8 steps with string arguments and knows no wave tool.
func TestCatalogV10OlderCatalogsUnchanged(t *testing.T) {
	v9 := parseFile(t, goldenV9)
	for name, c := range map[string][2]string{
		"plan of 9":          {"show_plan", planOf(t, maxPlanSteps+1, map[string]any{"tool": "list_subjects"})},
		"planned ids array":  {"show_plan", planOf(t, 1, map[string]any{"tool": "job_status", "arguments": map[string]any{"ids": []string{"job-1"}}})},
		"mark":               {"mark_assignments_done", `{"assignments":["asg-1"],"done":true}`},
		"button":             {"offer_button", `{"action":"open_help"}`},
		"state":              {"list_assignments", `{"state":"done"}`},
		"21 ids":             {"job_status", `{"ids":` + `["j1","j2","j3","j4","j5","j6","j7","j8","j9","j10","j11","j12","j13","j14","j15","j16","j17","j18","j19","j20","j21"]}`},
		"planned long query": {"show_plan", planOf(t, 1, map[string]any{"tool": "search", "arguments": map[string]any{"query": strings.Repeat("я", MaxLabelRunes+1)}})},
	} {
		if err := v9.Check(call(t, c[0], c[1])); !errors.Is(err, ErrInvalidCall) {
			t.Errorf("v9 %s: err = %v, want ErrInvalidCall", name, err)
		}
	}
	if err := v9.Check(call(t, "show_plan", planOf(t, maxPlanSteps, map[string]any{"tool": "list_subjects"}))); err != nil {
		t.Errorf("v9 plan of 8: %v", err)
	}
}

// TestCatalogV10ArgumentLimit: a returned show_plan of catalog 10 takes arguments up to 32 KiB, every other tool and
// every older catalog 8 KiB.
func TestCatalogV10ArgumentLimit(t *testing.T) {
	v9, v10 := parseFile(t, goldenV9), parseFile(t, goldenV10)
	steps := 150 // each step about 160 bytes: well over 8 KiB, under 32 KiB
	plan := planOf(t, steps, map[string]any{"tool": "change_assignment", "arguments": map[string]any{
		"assignment": "asg-0123456789abcdef", "action": "edit", "due": "2026-11-10", "time": "09:00", "title": "Решить номер 5",
	}})
	if len(plan) <= values.MaxArgumentsBytes || len(plan) > values.MaxPlanArgumentsBytes {
		t.Fatalf("plan of %d bytes", len(plan))
	}
	big := mustCall(t, "toolu_1", "show_plan", plan)
	if err := v10.CheckReturned([]values.ToolCall{big}); err != nil {
		t.Errorf("v10 plan of %d bytes: %v", len(plan), err)
	}
	if err := v9.CheckReturned([]values.ToolCall{big}); !errors.Is(err, ErrInvalidCall) {
		t.Errorf("v9 plan of %d bytes: err = %v", len(plan), err)
	}

	// A note of 4000 three-byte runes is a valid reply_text over 8 KiB: refused for its size alone.
	b, err := json.Marshal(map[string]any{"action": "add_text", "date": "2026-11-04", "text": strings.Repeat("漢", MaxReplyTextRunes)})
	if err != nil {
		t.Fatal(err)
	}
	long, err := values.NewReturnedToolCall("toolu_2", "note", string(b))
	if err != nil {
		t.Fatal(err)
	}
	if err := v10.Check(long); err != nil {
		t.Fatalf("the note itself: %v", err)
	}
	if err := v10.CheckReturned([]values.ToolCall{long}); !errors.Is(err, ErrInvalidCall) {
		t.Errorf("v10 note of %d bytes: err = %v", len(b), err)
	}
}

// TestCatalogV10OnlyFromCatalog10: the wave's catalog features in a catalog marked older, or used elsewhere, are
// catalog errors; every released catalog still parses.
func TestCatalogV10OnlyFromCatalog10(t *testing.T) {
	raw, err := os.ReadFile(goldenV10)
	if err != nil {
		t.Fatal(err)
	}
	v10 := string(raw)
	for name, text := range map[string]string{
		"v10 marked 9": strings.Replace(v10, `"catalog_version": 10,`, `"catalog_version": 9,`, 1),
		"max_items 51": strings.Replace(v10, `"max_items": 50`, `"max_items": 51`, 1),
		"max_items on an id": strings.Replace(v10, `{"name": "assignment", "type": "id", "required": true},
      {"name": "action", "type": "enum", "required": true, "values": ["edit"`, `{"name": "assignment", "type": "id", "required": true, "max_items": 5},
      {"name": "action", "type": "enum", "required": true, "values": ["edit"`, 1),
		"reply_text on note.title": strings.Replace(v10, `{"name": "title", "type": "label", "required": false}
    ]},
    {"name": "get_settings"`, `{"name": "title", "type": "reply_text", "required": false}
    ]},
    {"name": "get_settings"`, 1),
		"bool on a card": strings.Replace(v10, `{"name": "notes", "type": "text_list", "required": false}`, `{"name": "notes", "type": "bool", "required": false}`, 1),
		"enum on reply":  strings.Replace(v10, `{"name": "assignments", "type": "card_ids", "required": false}`, `{"name": "assignments", "type": "enum", "required": false, "values": ["a"]}`, 1),
		"label on the button": strings.Replace(v10, `{"name": "work", "type": "id", "required": false}`,
			`{"name": "work", "type": "label", "required": false}`, 1),
	} {
		if text == v10 {
			t.Fatalf("%s: the mutation did not apply", name)
		}
		if _, err := Parse([]byte(text)); !errors.Is(err, ErrInvalidCatalog) {
			t.Errorf("%s: err = %v, want ErrInvalidCatalog", name, err)
		}
	}
	for _, file := range []string{goldenV1, goldenV2, goldenV3, golden, goldenV5, goldenV6, goldenV7, goldenV9, goldenV10} {
		parseFile(t, file)
	}
}
