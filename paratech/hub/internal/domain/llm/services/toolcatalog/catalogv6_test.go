package toolcatalog

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

const goldenV6 = "../../../../../../shared/testdata/tool_catalog_v6.json"

// The date description of catalog 6 names create_assignment.due; it is part of every date argument's schema.
const (
	dateV5 = "as a deadline (edit_due.due) it means"
	dateV6 = "as a deadline (edit_due.due, create_assignment.due without time) it means"
)

// TestCatalogV6 (TASK-216): v6 is v5 plus create_assignment (U, free) after edit_due; the other tools keep their
// schemas but for the new date description and the plan step that may name the new tool.
func TestCatalogV6(t *testing.T) {
	v5, v6 := parseFile(t, goldenV5), parseFile(t, goldenV6)
	if v6.Version() != 6 || !v6.Has("create_assignment") || v5.Has("create_assignment") || len(v6.Specs()) != len(v5.Specs())+1 {
		t.Fatalf("v6: version %d, %d tools", v6.Version(), len(v6.Specs()))
	}
	if v6.tools["create_assignment"].Paid || !v6.tools["create_assignment"].AgentCanExecute || v6.tools["create_assignment"].Class != classUndo {
		t.Fatalf("create_assignment = %+v", v6.tools["create_assignment"])
	}
	if strings.Join(v6.order, ",") != strings.Replace(strings.Join(v5.order, ","), "edit_due,", "edit_due,create_assignment,", 1) {
		t.Fatalf("v6 order = %v", v6.order)
	}
	for _, old := range v5.Specs() {
		want := strings.ReplaceAll(old.Schema(), dateV5, dateV6)
		if old.Name() == "show_plan" {
			want = strings.Replace(want, `"edit_due","triage_assignment"`, `"edit_due","create_assignment","triage_assignment"`, 1)
		}
		got := specFor(t, v6, old.Name())
		if got.Schema() != want || got.Description() != old.Description() {
			t.Errorf("%s changed beyond the date description:\nv5 %s\nv6 %s", old.Name(), old.Schema(), got.Schema())
		}
	}
	if d := specFor(t, v6, "create_assignment").Description(); !strings.HasPrefix(d, "Reversible local action") || strings.Contains(d, "Paid") {
		t.Errorf("create_assignment description = %q", d)
	}

	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal([]byte(specFor(t, v6, "create_assignment").Schema()), &schema); err != nil {
		t.Fatal(err)
	}
	tm := schema.Properties["time"]
	if len(schema.Properties) != 6 || len(schema.Required) != 1 || schema.Required[0] != "title" ||
		tm["type"] != "string" || tm["pattern"] != timePattern || !strings.Contains(tm["description"].(string), "only together with due") {
		t.Fatalf("create_assignment schema = %+v", schema)
	}
	if schema.Properties["title"]["maxLength"] != float64(MaxLabelRunes) || schema.Properties["note"]["maxLength"] != float64(MaxQueryRunes) ||
		schema.Properties["due"]["pattern"] != datePattern {
		t.Fatalf("create_assignment schema = %+v", schema)
	}
}

func TestCreateAssignmentCalls(t *testing.T) {
	c := parseFile(t, goldenV6)
	for _, args := range []string{
		`{"title":"Скинуть проверочную Иванову","subject":"s1","due":"2026-10-23","time":"09:00"}`,
		`{"title":"Решить номер 5","subject":"s2","lesson":"l-7","due":"2026-10-30","note":"Задачник Демидовича"}`,
		`{"title":"Прочитать главу 3"}`,
		`{"title":"t","due":"2026-10-23","time":"00:00"}`,
		`{"title":"t","due":"2026-10-23","time":"23:59"}`,
		`{"title":"t","due":"2026-10-23","time":"19:05"}`,
		// time without due is the desktop's refusal (an is_error result), not a Hub one
		`{"title":"t","time":"09:00"}`,
	} {
		if err := c.Check(call(t, "create_assignment", args)); err != nil {
			t.Errorf("%s: %v", args, err)
		}
	}
	for name, args := range map[string]string{
		"time 9:00":       `{"title":"t","due":"2026-10-23","time":"9:00"}`,
		"time 24:00":      `{"title":"t","due":"2026-10-23","time":"24:00"}`,
		"time 09:60":      `{"title":"t","due":"2026-10-23","time":"09:60"}`,
		"time seconds":    `{"title":"t","due":"2026-10-23","time":"09:00:00"}`,
		"time no colon":   `{"title":"t","due":"2026-10-23","time":"0900"}`,
		"time space":      `{"title":"t","due":"2026-10-23","time":" 09:00"}`,
		"time newline":    `{"title":"t","due":"2026-10-23","time":"09:00\n"}`,
		"time pm":         `{"title":"t","due":"2026-10-23","time":"9pm"}`,
		"time number":     `{"title":"t","due":"2026-10-23","time":900}`,
		"time empty":      `{"title":"t","due":"2026-10-23","time":""}`,
		"no title":        `{"due":"2026-10-23"}`,
		"blank title":     `{"title":"  "}`,
		"long title":      `{"title":"` + strings.Repeat("я", MaxLabelRunes+1) + `"}`,
		"title newline":   `{"title":"a\nb"}`,
		"long note":       `{"title":"t","note":"` + strings.Repeat("я", MaxQueryRunes+1) + `"}`,
		"note newline":    `{"title":"t","note":"a\nb"}`,
		"bad due":         `{"title":"t","due":"2026-02-30"}`,
		"subject a title": `{"title":"t","subject":"Мат анализ"}`,
		"email argument":  `{"title":"t","email":"ivanov@example.com"}`,
		"teacher":         `{"title":"t","teacher":"Иванов"}`,
	} {
		if err := c.Check(call(t, "create_assignment", args)); !errors.Is(err, ErrInvalidCall) {
			t.Errorf("%s: err = %v, want ErrInvalidCall", name, err)
		} else if strings.Contains(err.Error(), "ivanov") || strings.Contains(err.Error(), "я") {
			t.Errorf("%s: the error quotes the call: %v", name, err)
		}
	}
	if err := c.Check(call(t, "show_plan", `{"summary":"Два задания.","steps":[{"tool":"create_assignment","arguments":{"title":"Эссе"}}]}`)); err != nil {
		t.Errorf("a plan step of create_assignment: %v", err)
	}
	if err := parseFile(t, goldenV5).Check(call(t, "create_assignment", `{"title":"t"}`)); !errors.Is(err, ErrInvalidCall) {
		t.Errorf("v5 create_assignment: err = %v, want ErrInvalidCall", err)
	}
}

// TestPlanStepNoteLimit (TASK-221): a planned create_assignment note keeps the catalog's own limit, every other step
// argument stays a label of at most MaxLabelRunes.
func TestPlanStepNoteLimit(t *testing.T) {
	c := parseFile(t, goldenV6)
	plan := func(tool, args string) string {
		return `{"summary":"План.","steps":[{"tool":"` + tool + `","arguments":` + args + `}]}`
	}
	note := func(n int) string { return `{"title":"t","note":"` + strings.Repeat("я", n) + `"}` }

	for name, args := range map[string]string{
		"note 81":    note(MaxLabelRunes + 1),
		"note limit": note(MaxQueryRunes),
	} {
		if err := c.Check(call(t, "show_plan", plan("create_assignment", args))); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	for name, args := range map[string]string{
		"note over limit": note(MaxQueryRunes + 1),
		"note newline":    `{"note":"a\nb"}`,
		"note blank":      `{"note":"  "}`,
		"note number":     `{"note":5}`,
		"title over 80":   `{"title":"` + strings.Repeat("я", MaxLabelRunes+1) + `"}`,
		"subject over 80": `{"subject":"` + strings.Repeat("я", MaxLabelRunes+1) + `"}`,
	} {
		if err := c.Check(call(t, "show_plan", plan("create_assignment", args))); !errors.Is(err, ErrInvalidCall) {
			t.Errorf("%s: err = %v, want ErrInvalidCall", name, err)
		}
	}

	if err := c.Check(call(t, "show_plan", plan("edit_due", note(MaxLabelRunes+1)))); !errors.Is(err, ErrInvalidCall) {
		t.Errorf("note of another tool over 80: err = %v, want ErrInvalidCall", err)
	}
}

// TestTimeTypeOnlyFromCatalog6: the time type in a catalog before 6 is a catalog error, so a released catalog cannot
// grow it; UI tools never take it.
func TestTimeTypeOnlyFromCatalog6(t *testing.T) {
	read := func(file string) string {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	v5, v6 := read(goldenV5), read(goldenV6)
	edit := `{"name": "due", "type": "date", "required": true}`
	for name, text := range map[string]string{
		"time in v5":        strings.Replace(v5, edit, `{"name": "due", "type": "time", "required": true}`, 1),
		"v6 marked 5":       strings.Replace(v6, `"catalog_version": 6,`, `"catalog_version": 5,`, 1),
		"v6 marked 1":       strings.Replace(v6, `"catalog_version": 6,`, `"catalog_version": 1,`, 1),
		"time on a UI tool": strings.Replace(v6, `{"name": "text", "type": "text", "required": true}`, `{"name": "text", "type": "time", "required": true}`, 1),
		"time with values":  strings.Replace(v6, `{"name": "time", "type": "time", "required": false}`, `{"name": "time", "type": "time", "required": false, "values": ["morning"]}`, 1),
	} {
		if text == v5 || text == v6 {
			t.Fatalf("%s: the mutation did not apply", name)
		}
		_, err := Parse([]byte(text))
		if !errors.Is(err, ErrInvalidCatalog) {
			t.Errorf("%s: err = %v, want ErrInvalidCatalog", name, err)
		}
		if strings.HasPrefix(name, "v") || name == "time in v5" {
			if err == nil || !strings.Contains(err.Error(), "the time type exists from catalog 6") {
				t.Errorf("%s: err = %v, want the time type refused", name, err)
			}
		}
	}
	for _, file := range []string{goldenV1, goldenV2, goldenV3, golden, goldenV5, goldenV6} {
		parseFile(t, file) // every released catalog stays served
	}
}
