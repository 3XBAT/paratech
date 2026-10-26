package values

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestParseTimeOfDay: the catalog type time (v6) is exactly HH:MM, 24 h, as the Hub's pattern.
func TestParseTimeOfDay(t *testing.T) {
	for _, tt := range []struct {
		in, want string
	}{
		{"00:00", "00:00"}, {"09:00", "09:00"}, {"21:30", "21:30"}, {"23:59", "23:59"},
	} {
		got, err := ParseTimeOfDay(tt.in)
		if err != nil || got.String() != tt.want || got.IsZero() {
			t.Errorf("ParseTimeOfDay(%q) = %v, %v", tt.in, got, err)
		}
	}
	for _, bad := range []string{"", "9:00", "24:00", "09:60", "09-00", "0900", "09:00:00", " 09:00", "٠٩:٠٠", "ab:cd"} {
		if _, err := ParseTimeOfDay(bad); !errors.Is(err, ErrInvalidTime) {
			t.Errorf("ParseTimeOfDay(%q) = %v, want ErrInvalidTime", bad, err)
		}
	}
	if (TimeOfDay{}).String() != "" || !(TimeOfDay{}).IsZero() {
		t.Fatal("zero time")
	}
	nine, err := NewTimeOfDay(9, 5)
	if err != nil || nine.Hour() != 9 || nine.Minute() != 5 {
		t.Fatalf("NewTimeOfDay = %v, %v", nine, err)
	}
}

// TestCreateAssignmentCall: the schema of create_assignment (v6). A time needs its due date, the title is a one-line
// label, the note a query, and the model can name no ID of the new assignment (no such argument).
func TestCreateAssignmentCall(t *testing.T) {
	call, err := ParseCall("c1", "create_assignment",
		`{"title":"Скинуть проверочную Иванову","due":"2026-10-28","time":"09:00","subject":"s1","note":"с решениями"}`)
	if err != nil {
		t.Fatal(err)
	}
	args := call.Args()
	title, _ := args.Label("title")
	note, _ := args.Query("note")
	due, _ := args.Date("due")
	tod, ok := args.Time("time")
	if title != "Скинуть проверочную Иванову" || note != "с решениями" || due.String() != "2026-10-28" || !ok || tod.String() != "09:00" {
		t.Fatalf("args = %q %q %v %v", title, note, due, tod)
	}
	if refs := call.Refs(); len(refs) != 1 || refs[0].Kind() != KindSubject {
		t.Fatalf("refs = %v", refs)
	}

	for name, args := range map[string]string{
		"time without due":    `{"title":"Скинуть","time":"09:00"}`,
		"time with null due":  `{"title":"Скинуть","due":null,"time":"09:00"}`,
		"time 9:00":           `{"title":"Скинуть","due":"2026-10-28","time":"9:00"}`,
		"time 24:00":          `{"title":"Скинуть","due":"2026-10-28","time":"24:00"}`,
		"time 09:60":          `{"title":"Скинуть","due":"2026-10-28","time":"09:60"}`,
		"no title":            `{"due":"2026-10-28"}`,
		"title two lines":     `{"title":"Скинуть\nпроверочную"}`,
		"title too long":      `{"title":"` + strings.Repeat("я", MaxLabelRunes+1) + `"}`,
		"model-chosen id":     `{"title":"Скинуть","id":"a1"}`,
		"model-chosen key":    `{"title":"Скинуть","assignment":"a1"}`,
		"recipient":           `{"title":"Скинуть","to":"ivanov@example.com"}`,
		"subject of a lesson": `{"title":"Скинуть","subject":"l/1"}`,
	} {
		if _, err := ParseCall("c1", "create_assignment", args); !errors.Is(err, ErrInvalidArguments) {
			t.Errorf("%s: %v, want ErrInvalidArguments", name, err)
		}
	}

	// An address typed in the title or note is the student's text: accepted as text, never an ID or a recipient.
	mail, err := ParseCall("c1", "create_assignment", `{"title":"Отправить Иванову на ivanov@example.com","note":"ivanov@example.com"}`)
	if err != nil || len(mail.Refs()) != 0 {
		t.Fatalf("title with an address = %v, refs %v", err, mail.Refs())
	}
}

// TestPlanStepOfCreateAssignment: a plan step stores and compares the title, due and time; the note is free text the
// step cannot carry (plan-step arguments are at most 80 characters), so matching ignores it for this tool only. An ID
// the step creates is deferred in propose_deadline and edit_due after it.
func TestPlanStepOfCreateAssignment(t *testing.T) {
	stored := `[{"tool":"create_assignment","arguments":{"due":"2026-10-28","time":"09:00","title":"Скинуть проверочную"}},` +
		`{"tool":"propose_deadline"},{"tool":"edit_due","arguments":{"due":"2026-10-29"}}]`
	steps, err := DecodePlanSteps(stored)
	if err != nil {
		t.Fatal(err)
	}
	if enc, err := EncodePlanSteps(steps); err != nil || enc != stored {
		t.Fatalf("EncodePlanSteps = %s, %v", enc, err)
	}
	if !slices.Equal(steps[1].Deferred(), []string{"assignment"}) || !slices.Equal(steps[2].Deferred(), []string{"assignment"}) {
		t.Fatalf("deferred = %v, %v", steps[1].Deferred(), steps[2].Deferred())
	}
	if k, ok := CreatedKind(ToolCreateAssignment); !ok || k != KindAssignment || !IsCreatedKind(KindAssignment) || IsCreatedKind(KindWork) {
		t.Fatal("create_assignment does not create an assignment for later steps")
	}

	step := steps[0]
	for name, tt := range map[string]struct {
		args string
		want bool
	}{
		"same":                 {`{"title":"Скинуть проверочную","due":"2026-10-28","time":"09:00"}`, true},
		"same with a note":     {`{"title":"Скинуть проверочную","due":"2026-10-28","time":"09:00","note":"` + strings.Repeat("н", 200) + `"}`, true},
		"other time":           {`{"title":"Скинуть проверочную","due":"2026-10-28","time":"10:00"}`, false},
		"no time":              {`{"title":"Скинуть проверочную","due":"2026-10-28"}`, false},
		"other due":            {`{"title":"Скинуть проверочную","due":"2026-10-29","time":"09:00"}`, false},
		"other title":          {`{"title":"Скинуть контрольную","due":"2026-10-28","time":"09:00"}`, false},
		"an unplanned subject": {`{"title":"Скинуть проверочную","due":"2026-10-28","time":"09:00","subject":"s1"}`, false},
	} {
		call, err := ParseCall("c1", "create_assignment", tt.args)
		if err != nil {
			t.Fatal(err)
		}
		if got := step.Matches(call); got != tt.want {
			t.Errorf("%s: Matches = %v, want %v", name, got, tt.want)
		}
	}

	// A planned note is ignored too: the call may change or drop it.
	withNote, err := DecodePlanSteps(`[{"tool":"create_assignment","arguments":{"title":"Эссе","note":"кратко"}}]`)
	if err != nil {
		t.Fatal(err)
	}
	if bare, _ := ParseCall("c1", "create_assignment", `{"title":"Эссе"}`); !withNote[0].Matches(bare) {
		t.Fatal("a call without the planned note does not match")
	}
	// Only create_assignment ignores free text: another tool's query still has to match.
	topic, _ := DecodePlanSteps(`[{"tool":"search","arguments":{"query":"закон Ома"}}]`)
	if other, _ := ParseCall("c1", "search", `{"query":"закон Ампера"}`); topic[0].Matches(other) {
		t.Fatal("a search step matched another query")
	}

	if _, err := DecodePlanSteps(`[{"tool":"create_assignment","arguments":{"title":"Эссе","time":"09:00"}}]`); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("a plan step with a time but no due = %v", err)
	}
	if _, err := DecodePlanSteps(`[{"tool":"create_assignment","arguments":{"title":"Эссе","due":"2026-10-28","time":"25:00"}}]`); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("a plan step with a bad time = %v", err)
	}
}

// TestActionLineOfAnAddedAssignment: an action line names the assignment it added only with its title from the local
// data; a time needs its date.
func TestActionLineOfAnAddedAssignment(t *testing.T) {
	a, _ := NewRef(KindAssignment, "ag-1")
	s, _ := NewRef(KindSubject, "s1")
	w, _ := NewRef(KindWork, "w1")
	due, _ := NewDate(2026, time.October, 28)
	nine, _ := NewTimeOfDay(9, 0)
	for name, tt := range map[string]struct {
		line ActionLine
		ok   bool
	}{
		"other tool":              {ActionLine{Action: "act1"}, true},
		"full":                    {ActionLine{Action: "act1", Assignment: AssignmentLine{Assignment: a, Title: "Эссе", Subject: s, Due: due, DueTime: nine}}, true},
		"no subject or due":       {ActionLine{Action: "act1", Assignment: AssignmentLine{Assignment: a, Title: "Эссе"}}, true},
		"not filled":              {ActionLine{Action: "act1", Assignment: AssignmentLine{Assignment: a}}, false},
		"time without due":        {ActionLine{Action: "act1", Assignment: AssignmentLine{Assignment: a, Title: "Эссе", DueTime: nine}}, false},
		"not an assignment":       {ActionLine{Action: "act1", Assignment: AssignmentLine{Assignment: w, Title: "Эссе"}}, false},
		"title without id":        {ActionLine{Action: "act1", Assignment: AssignmentLine{Title: "Эссе"}}, false},
		"subject of another kind": {ActionLine{Action: "act1", Assignment: AssignmentLine{Assignment: a, Title: "Эссе", Subject: w}}, false},
	} {
		if err := tt.line.Validate(); (err == nil) != tt.ok {
			t.Errorf("%s: Validate = %v", name, err)
		}
	}
}
