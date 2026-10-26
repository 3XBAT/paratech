package values

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParseCall(t *testing.T) {
	long := strings.Repeat("я", MaxLabelRunes+1)
	tests := []struct {
		name    string
		callID  string
		tool    string
		args    string
		wantErr error
	}{
		// accepted
		{name: "no arguments", tool: "list_subjects", args: `{}`},
		{name: "optional filters", tool: "find_lessons", args: `{"subject":"s1","from":"2026-10-01","to":"2026-10-31","kind":"lecture"}`},
		{name: "null optional is absent", tool: "find_lessons", args: `{"subject":null}`},
		{name: "ids", tool: "job_status", args: `{"ids":["j1","j2"]}`},
		{name: "label", tool: "search", args: `{"query":"производная сложной функции","scope":"all"}`},
		{name: "subject name", tool: "create_subject", args: `{"name":"Матан (2 курс)","semester":"Осень 2026"}`},
		{name: "folder name", tool: "create_folder", args: `{"subject":"s1","name":"Билеты, экзамен"}`},
		{name: "place into folder", tool: "place_upload", args: `{"upload":"u1","subject":"s1","folder":"f1"}`},
		{name: "place into lesson", tool: "place_upload", args: `{"upload":"u1","subject":"s1","lesson":"l1"}`},
		{name: "summary of recording", tool: "make_summary", args: `{"recording":"r1"}`},
		{name: "homework of material", tool: "find_homework", args: `{"material":"m1"}`},
		{name: "triage upload", tool: "triage_assignment", args: `{"upload":"u1"}`},
		{name: "triage topic", tool: "triage_assignment", args: `{"topic":"Презентация «Реформы Петра I», 10 слайдов"}`},
		{name: "due date", tool: "edit_due", args: `{"assignment":"a1","due":"2026-11-03"}`},
		{name: "reply text with newline", tool: "reply", args: `{"text":"Готово.\nКонспект в папке."}`},
		{name: "cannot do", tool: "cannot_do", args: `{"your_part":["Сдать лично"],"our_part":["Черновик"]}`},
		{name: "cannot do, one part empty", tool: "cannot_do", args: `{"your_part":[],"our_part":["Черновик"]}`},
		{name: "longest text list", tool: "cannot_do", args: `{"your_part":[` + strings.Repeat(`"a",`, MaxTextListItems-1) + `"a"],"our_part":[]}`},
		{name: "plan with empty notes", tool: "show_plan", args: `{"summary":"x","steps":[{"tool":"create_subject","arguments":{"name":"Химия"}}],"notes":[]}`},
		{name: "whitespace around object", tool: "list_subjects", args: " {} \n"},

		// refused
		{name: "unknown tool", tool: "send_email", args: `{}`, wantErr: ErrUnknownTool},
		{name: "bad call id", callID: "a b", tool: "list_subjects", args: `{}`, wantErr: ErrInvalidArguments},
		{name: "not an object", tool: "list_subjects", args: `[]`, wantErr: ErrInvalidArguments},
		{name: "empty", tool: "list_subjects", args: ``, wantErr: ErrInvalidArguments},
		{name: "trailing data", tool: "list_subjects", args: `{}{}`, wantErr: ErrInvalidArguments},
		{name: "duplicate key", tool: "delivery_status", args: `{"assignment":"a1","assignment":"a2"}`, wantErr: ErrInvalidArguments},
		{name: "unknown argument", tool: "delivery_status", args: `{"assignment":"a1","email":"x@y"}`, wantErr: ErrInvalidArguments},
		{name: "missing required", tool: "delivery_status", args: `{}`, wantErr: ErrInvalidArguments},
		{name: "null required", tool: "delivery_status", args: `{"assignment":null}`, wantErr: ErrInvalidArguments},
		{name: "id as number", tool: "delivery_status", args: `{"assignment":12}`, wantErr: ErrInvalidArguments},
		{name: "id with slash", tool: "delivery_status", args: `{"assignment":"../a"}`, wantErr: ErrInvalidArguments},
		{name: "id with space", tool: "delivery_status", args: `{"assignment":"a 1"}`, wantErr: ErrInvalidArguments},
		{name: "enum outside", tool: "find_lessons", args: `{"kind":"exam"}`, wantErr: ErrInvalidArguments},
		{name: "enum case", tool: "find_lessons", args: `{"kind":"Lecture"}`, wantErr: ErrInvalidArguments},
		{name: "bad date", tool: "edit_due", args: `{"assignment":"a1","due":"2026-02-30"}`, wantErr: ErrInvalidArguments},
		{name: "short date", tool: "edit_due", args: `{"assignment":"a1","due":"2026-1-1"}`, wantErr: ErrInvalidArguments},
		{name: "date with time", tool: "edit_due", args: `{"assignment":"a1","due":"2026-11-03T10:00"}`, wantErr: ErrInvalidArguments},
		{name: "to before from", tool: "find_lessons", args: `{"from":"2026-10-31","to":"2026-10-01"}`, wantErr: ErrInvalidArguments},
		// generate_draft left the catalog in v4: the planner's call is an unknown tool.
		{name: "retired tool", tool: "generate_draft", args: `{"assignment":"a1"}`, wantErr: ErrUnknownTool},
		{name: "name too long", tool: "create_subject", args: `{"name":"` + long + `"}`, wantErr: ErrInvalidArguments},
		// search.query is a query of up to 500 characters since catalog v2 (TASK-118).
		{name: "query too long", tool: "search", args: `{"query":"` + strings.Repeat("я", MaxQueryRunes+1) + `"}`, wantErr: ErrInvalidArguments},
		// triage_assignment.topic (catalog v4) is free model text: a query, checked like one.
		{name: "topic too long", tool: "triage_assignment", args: `{"topic":"` + strings.Repeat("я", MaxQueryRunes+1) + `"}`, wantErr: ErrInvalidArguments},
		{name: "topic with newline", tool: "triage_assignment", args: `{"topic":"a\nb"}`, wantErr: ErrInvalidArguments},
		{name: "topic with bidi override", tool: "triage_assignment", args: `{"topic":"a\u202eb"}`, wantErr: ErrInvalidArguments},
		{name: "topic with html", tool: "triage_assignment", args: `{"topic":"<b>x</b>"}`, wantErr: ErrInvalidArguments},
		{name: "blank topic", tool: "triage_assignment", args: `{"topic":"  "}`, wantErr: ErrInvalidArguments},
		{name: "topic as list", tool: "triage_assignment", args: `{"topic":["a"]}`, wantErr: ErrInvalidArguments},
		{name: "label with newline", tool: "search", args: `{"query":"a\nb"}`, wantErr: ErrInvalidArguments},
		{name: "label with bidi override", tool: "search", args: `{"query":"a\u202eb"}`, wantErr: ErrInvalidArguments},
		{name: "label with zero width", tool: "search", args: `{"query":"a\u200bb"}`, wantErr: ErrInvalidArguments},
		{name: "label with html", tool: "search", args: `{"query":"<b>x</b>"}`, wantErr: ErrInvalidArguments},
		{name: "blank label", tool: "search", args: `{"query":"   "}`, wantErr: ErrInvalidArguments},
		{name: "ids empty", tool: "job_status", args: `{"ids":[]}`, wantErr: ErrInvalidArguments},
		{name: "ids duplicate", tool: "job_status", args: `{"ids":["j1","j1"]}`, wantErr: ErrInvalidArguments},
		{name: "ids not array", tool: "job_status", args: `{"ids":"j1"}`, wantErr: ErrInvalidArguments},
		{name: "text too long", tool: "reply", args: `{"text":"` + strings.Repeat("x", MaxTextRunes+1) + `"}`, wantErr: ErrInvalidArguments},
		{name: "text with html", tool: "reply", args: `{"text":"<script>x</script>"}`, wantErr: ErrInvalidArguments},
		{name: "text list too long", tool: "cannot_do", args: `{"your_part":[` + strings.Repeat(`"a",`, MaxTextListItems) + `"a"],"our_part":["b"]}`, wantErr: ErrInvalidArguments},
		{name: "cannot do with nothing", tool: "cannot_do", args: `{"your_part":[],"our_part":[]}`, wantErr: ErrInvalidArguments},
		{name: "too large", tool: "search", args: `{"query":"a","x":"` + strings.Repeat("x", MaxArgumentsBytes) + `"}`, wantErr: ErrInvalidArguments},

		// reply.assignments (card_ids, catalog v3)
		{name: "reply with assignments", tool: "reply", args: `{"text":"Два задания.","assignments":["a1","a2"]}`},
		{name: "reply with no assignments", tool: "reply", args: `{"text":"Заданий нет.","assignments":[]}`},
		{name: "reply with null assignments", tool: "reply", args: `{"text":"Заданий нет.","assignments":null}`},
		{name: "reply with ten assignments", tool: "reply", args: `{"text":"x","assignments":[` + strings.Repeat(`"a1",`, MaxCardIDs-1) + `"a2"]}`},
		{name: "reply with eleven assignments", tool: "reply", args: `{"text":"x","assignments":[` + strings.Repeat(`"a1",`, MaxCardIDs) + `"a2"]}`, wantErr: ErrInvalidArguments},
		{name: "assignments not a list", tool: "reply", args: `{"text":"x","assignments":"a1"}`, wantErr: ErrInvalidArguments},
		{name: "assignment as number", tool: "reply", args: `{"text":"x","assignments":[12]}`, wantErr: ErrInvalidArguments},
		{name: "assignment as title", tool: "reply", args: `{"text":"x","assignments":["Эссе по истории"]}`, wantErr: ErrInvalidArguments},
		{name: "assignment as object", tool: "reply", args: `{"text":"x","assignments":[{"id":"a1","title":"Эссе"}]}`, wantErr: ErrInvalidArguments},
		{name: "assignments without text", tool: "reply", args: `{"assignments":["a1"]}`, wantErr: ErrInvalidArguments},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := tt.callID
			if id == "" {
				id = "toolu_01"
			}
			call, err := ParseCall(id, tt.tool, tt.args)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ParseCall: %v", err)
				}
				if call.Tool() != Tool(tt.tool) || call.ID() != id {
					t.Fatalf("call = %v %v", call.Tool(), call.ID())
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseCall error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestCardIDs: the assignments of a reply keep the planner's order, are typed and shown once each, and go into Refs so
// the provenance check sees every one.
func TestCardIDs(t *testing.T) {
	call, err := ParseCall("c1", "reply", `{"text":"x","assignments":["a2","a1","a2"]}`)
	if err != nil {
		t.Fatal(err)
	}
	a1, a2 := mustRef(t, KindAssignment, "a1"), mustRef(t, KindAssignment, "a2")
	if got := call.Args().CardIDs("assignments"); len(got) != 2 || got[0] != a2 || got[1] != a1 {
		t.Fatalf("CardIDs = %v", got)
	}
	if refs := call.Refs(); len(refs) != 2 || refs[0] != a1 || refs[1] != a2 {
		t.Fatalf("Refs = %v", refs)
	}
	if call.Args().IDs("assignments") != nil || call.Args().CardIDs("text") != nil {
		t.Fatal("accessors of other types return card ids")
	}

	empty, err := ParseCall("c2", "reply", `{"text":"x","assignments":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Args().CardIDs("assignments") != nil || len(empty.Refs()) != 0 {
		t.Fatal("an empty list has ids")
	}
}

// TestParseCallOneOfGroups: exactly one member of a one-of group, for the pairs and the three-way group of
// triage_assignment (catalog v4).
func TestParseCallOneOfGroups(t *testing.T) {
	tests := []struct {
		tool    string
		base    string
		members []string
	}{
		{tool: "make_summary", members: []string{`"recording":"r1"`, `"material":"m1"`}},
		{tool: "find_homework", members: []string{`"recording":"r1"`, `"material":"m1"`}},
		{tool: "triage_assignment", members: []string{`"assignment":"a1"`, `"upload":"u1"`, `"topic":"Реформы Петра I"`}},
		{tool: "place_upload", base: `"upload":"u1","subject":"s1"`, members: []string{`"folder":"f1"`, `"lesson":"l1"`}},
	}
	join := func(parts ...string) string {
		var keep []string
		for _, p := range parts {
			if p != "" {
				keep = append(keep, p)
			}
		}
		return "{" + strings.Join(keep, ",") + "}"
	}
	for _, tt := range tests {
		type check struct {
			name string
			args string
			ok   bool
		}
		cases := []check{
			{"none", join(tt.base), false},
			{"all", join(append([]string{tt.base}, tt.members...)...), false},
		}
		for i, m := range tt.members {
			cases = append(cases, check{fmt.Sprintf("only %d", i), join(tt.base, m), true})
			for j := i + 1; j < len(tt.members); j++ {
				cases = append(cases, check{fmt.Sprintf("%d and %d", i, j), join(tt.base, m, tt.members[j]), false})
			}
		}
		for _, c := range cases {
			t.Run(tt.tool+"/"+c.name, func(t *testing.T) {
				_, err := ParseCall("c1", tt.tool, c.args)
				if c.ok != (err == nil) {
					t.Fatalf("ParseCall(%s) error = %v, want ok=%v", c.args, err, c.ok)
				}
				if err != nil && !errors.Is(err, ErrInvalidArguments) {
					t.Fatalf("error %v is not ErrInvalidArguments", err)
				}
			})
		}
	}
}

func TestParseCallPersistentNames(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"cyrillic with digits", "Физика 2", true},
		{"allowed punctuation", "Англ. яз (B2), группа-1", true},
		{"trimmed", "  Химия  ", true},
		{"slash", "a/b", false},
		{"backslash", `a\\b`, false},
		{"colon", "C:x", false},
		{"device name", "CON", false},
		{"device name with extension", "nul.txt", false},
		{"trailing dot", "Матан.", false},
		{"double space", "a  b", false},
		{"bidi override", "a\u202eb", false},
		{"control", "a\u0007b", false},
		{"quote", `a\"b`, false},
		{"emoji", "a\u2764", false},
		{"too long", strings.Repeat("я", MaxLabelRunes+1), false},
		{"html", "<b>", false},
	}
	for _, tt := range tests {
		for _, tool := range []string{"create_subject", "create_folder"} {
			t.Run(tool+"/"+tt.name, func(t *testing.T) {
				args := `{"name":"` + tt.in + `"}`
				if tool == "create_folder" {
					args = `{"subject":"s1","name":"` + tt.in + `"}`
				}
				call, err := ParseCall("c1", tool, args)
				if tt.ok != (err == nil) {
					t.Fatalf("ParseCall(%s) error = %v, want ok=%v", args, err, tt.ok)
				}
				if err == nil {
					if n, ok := call.Args().Name("name"); !ok || string(n) != strings.TrimSpace(tt.in) {
						t.Fatalf("name = %q, %v", n, ok)
					}
				}
			})
		}
	}
	// The semester of create_subject persists too.
	if _, err := ParseCall("c1", "create_subject", `{"name":"Физика","semester":"a/b"}`); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("semester with a slash: %v", err)
	}
}

func TestArgsAccessors(t *testing.T) {
	call, err := ParseCall("c1", "find_lessons", `{"subject":"s1","from":"2026-10-01","kind":"lab"}`)
	if err != nil {
		t.Fatal(err)
	}
	a := call.Args()
	if r, ok := a.ID("subject"); !ok || r.Kind() != KindSubject || r.ID() != "s1" {
		t.Fatalf("ID = %v %v", r, ok)
	}
	if d, ok := a.Date("from"); !ok || d.String() != "2026-10-01" {
		t.Fatalf("Date = %v %v", d, ok)
	}
	if e, ok := a.Enum("kind"); !ok || e != "lab" {
		t.Fatalf("Enum = %v %v", e, ok)
	}
	if a.Has("to") {
		t.Fatal("absent argument reported")
	}
	if _, ok := a.Label("subject"); ok {
		t.Fatal("typed accessor accepted another type")
	}
	if got := strings.Join(a.Names(), ","); got != "from,kind,subject" {
		t.Fatalf("Names = %s", got)
	}
	if refs := call.Refs(); len(refs) != 1 || refs[0].String() != "subject:s1" {
		t.Fatalf("Refs = %v", refs)
	}

	ids, err := ParseCall("c2", "job_status", `{"ids":["j2","j1"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids.Args().IDs("ids"); len(got) != 2 || got[0].Kind() != KindJob {
		t.Fatalf("IDs = %v", got)
	}
}

func TestPlanSteps(t *testing.T) {
	args := `{"summary":"Разложить файл и сделать конспект","steps":[` +
		`{"tool":"place_upload","arguments":{"upload":"u1","subject":"s1","folder":"f1"}},` +
		`{"tool":"make_summary","arguments":{"material":"m1"}},` +
		`{"tool":"create_subject","arguments":{"name":"Физика"}},` +
		`{"tool":"triage_assignment","arguments":{"assignment":"a1"}},` +
		`{"tool":"create_folder","arguments":{"name":"Лекции"}},` +
		`{"tool":"place_upload","arguments":{"upload":"u2","subject":"s1"}}],"notes":["Займёт пару минут"]}`
	call, err := ParseCall("c1", "show_plan", args)
	if err != nil {
		t.Fatalf("ParseCall: %v", err)
	}
	steps := call.Args().PlanSteps("steps")
	if len(steps) != 6 {
		t.Fatalf("steps = %d", len(steps))
	}
	refs := call.Refs()
	if len(refs) != 6 {
		t.Fatalf("plan refs = %v", refs)
	}

	match := []struct {
		name string
		tool string
		args string
		step int
		want bool
	}{
		{"same ids", "place_upload", `{"upload":"u1","subject":"s1","folder":"f1"}`, 0, true},
		{"other upload", "place_upload", `{"upload":"u2","subject":"s1","folder":"f1"}`, 0, false},
		{"other folder", "place_upload", `{"upload":"u1","subject":"s1","folder":"f2"}`, 0, false},
		{"lesson instead of folder", "place_upload", `{"upload":"u1","subject":"s1","lesson":"l1"}`, 0, false},
		{"other tool", "make_summary", `{"material":"m1"}`, 0, false},
		{"summary of material", "make_summary", `{"material":"m1"}`, 1, true},
		{"summary of recording", "make_summary", `{"recording":"r1"}`, 1, false},
		{"same name", "create_subject", `{"name":"Физика"}`, 2, true},
		{"other name", "create_subject", `{"name":"Химия"}`, 2, false},
		{"planned triage", "triage_assignment", `{"assignment":"a1"}`, 3, true},
		{"topic instead of the assignment", "triage_assignment", `{"topic":"Реформы Петра I"}`, 3, false},
		{"created subject", "create_folder", `{"subject":"s9","name":"Лекции"}`, 4, true},
		{"created subject, other name", "create_folder", `{"subject":"s9","name":"Семинары"}`, 4, false},
		{"created folder", "place_upload", `{"upload":"u2","subject":"s1","folder":"f9"}`, 5, true},
		{"lesson instead of created folder", "place_upload", `{"upload":"u2","subject":"s1","lesson":"l1"}`, 5, false},
	}
	for _, m := range match {
		t.Run(m.name, func(t *testing.T) {
			c, err := ParseCall("c2", m.tool, m.args)
			if err != nil {
				t.Fatal(err)
			}
			if got := steps[m.step].Matches(c); got != m.want {
				t.Fatalf("Matches = %v, want %v", got, m.want)
			}
		})
	}

	enc, err := EncodePlanSteps(steps)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := DecodePlanSteps(enc)
	if err != nil {
		t.Fatalf("DecodePlanSteps(%s): %v", enc, err)
	}
	again, err := EncodePlanSteps(dec)
	if err != nil || again != enc {
		t.Fatalf("round trip: %s != %s (%v)", again, enc, err)
	}
	if !strings.Contains(enc, `"arguments":{`) || strings.Contains(enc, `"args"`) {
		t.Fatalf("stored steps are not in the Hub shape: %s", enc)
	}
	if c, _ := ParseCall("c3", "create_folder", `{"subject":"s9","name":"Лекции"}`); !dec[4].Matches(c) {
		t.Fatal("decoded plan lost the deferred subject")
	}

	// Only an ID the step needs is deferred, and a call must fill in every deferred ID.
	lessonStep, err := DecodePlanSteps(`[{"tool":"create_folder","arguments":{"subject":"s1","name":"А"}},` +
		`{"tool":"place_upload","arguments":{"upload":"u1","subject":"s1","lesson":"l1"}}]`)
	if err != nil {
		t.Fatal(err)
	}
	if d := lessonStep[1].Deferred(); len(d) != 0 {
		t.Fatalf("folder deferred next to a planned lesson: %v", d)
	}
	if c, _ := ParseCall("c4", "place_upload", `{"upload":"u1","subject":"s1","lesson":"l1"}`); !lessonStep[1].Matches(c) {
		t.Fatal("the planned lesson step does not match")
	}
	if d := steps[5].Deferred(); len(d) != 1 || d[0] != "folder" {
		t.Fatalf("deferred of the created folder step = %v", d)
	}
	optional := PlanStep{tool: ToolFindLessons, args: Args{vals: map[string]argValue{}}, deferred: []string{"subject"}}
	if c, _ := ParseCall("c5", "find_lessons", `{}`); optional.Matches(c) {
		t.Fatal("a call without a deferred id matches")
	}

	bad := []struct{ name, steps string }{
		{"empty", `[]`},
		{"unknown tool", `[{"tool":"send_email"}]`},
		{"unknown key", `[{"tool":"make_summary","why":"x"}]`},
		{"args instead of arguments", `[{"tool":"make_summary","args":{"material":"m1"}}]`},
		{"text argument", `[{"tool":"reply","arguments":{"text":"x"}}]`},
		{"unknown argument", `[{"tool":"make_summary","arguments":{"path":"C:/x"}}]`},
		{"bad id", `[{"tool":"make_summary","arguments":{"material":"a/b"}}]`},
		{"duplicate key", `[{"tool":"make_summary","tool":"write_work"}]`},
		{"too many", `[` + strings.Repeat(`{"tool":"list_subjects"},`, MaxPlanSteps) + `{"tool":"list_subjects"}]`},
		{"no required argument", `[{"tool":"make_lecture_text"}]`},
		{"no due", `[{"tool":"edit_due","arguments":{"assignment":"a1"}}]`},
		{"no name", `[{"tool":"create_subject"}]`},
		{"neither of one-of", `[{"tool":"make_summary"}]`},
		{"both of one-of", `[{"tool":"make_summary","arguments":{"recording":"r1","material":"m1"}}]`},
		{"no folder or lesson", `[{"tool":"place_upload","arguments":{"upload":"u1","subject":"s1"}}]`},
		{"triage without a source", `[{"tool":"triage_assignment"}]`},
		{"triage with two sources", `[{"tool":"triage_assignment","arguments":{"assignment":"a1","topic":"Эссе"}}]`},
		{"retired tool", `[{"tool":"generate_draft","arguments":{"assignment":"a1"}}]`},
		{"subject created later", `[{"tool":"create_folder","arguments":{"name":"X"}},{"tool":"create_subject","arguments":{"name":"Y"}}]`},
		{"folder is not a subject", `[{"tool":"create_folder","arguments":{"subject":"s1","name":"X"}},{"tool":"create_folder","arguments":{"name":"Y"}}]`},
	}
	for _, b := range bad {
		t.Run("bad/"+b.name, func(t *testing.T) {
			_, err := ParseCall("c1", "show_plan", `{"summary":"x","steps":`+b.steps+`}`)
			if !errors.Is(err, ErrInvalidArguments) && !errors.Is(err, ErrUnknownTool) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestQuestions(t *testing.T) {
	tests := []struct {
		name string
		qs   string
		ok   bool
	}{
		{"choice and text", `[{"kind":"choice","question":"Какой формат?","options":["docx","pdf"],"default":0},{"kind":"text","question":"Сколько страниц?"}]`, true},
		{"default out of range", `[{"kind":"choice","question":"Q","options":["a","b"],"default":2}]`, false},
		{"negative default", `[{"kind":"choice","question":"Q","options":["a","b"],"default":-1}]`, false},
		{"fractional default", `[{"kind":"choice","question":"Q","options":["a","b"],"default":0.5}]`, false},
		{"default as string", `[{"kind":"choice","question":"Q","options":["a","b"],"default":"0"}]`, false},
		{"no default", `[{"kind":"choice","question":"Q","options":["a","b"]}]`, true},
		{"one option", `[{"kind":"choice","question":"Q","options":["a"],"default":0}]`, false},
		{"most options", `[{"kind":"choice","question":"Q","options":["1","2","3","4","5","6"],"default":5}]`, true},
		{"too many options", `[{"kind":"choice","question":"Q","options":["1","2","3","4","5","6","7"],"default":0}]`, false},
		{"text with options", `[{"kind":"text","question":"Q","options":["a","b"]}]`, false},
		{"text with empty options", `[{"kind":"text","question":"Q","options":[]}]`, true},
		{"text with default", `[{"kind":"text","question":"Q","default":0}]`, false},
		{"text instead of question", `[{"kind":"text","text":"Q"}]`, false},
		{"most questions", `[{"kind":"text","question":"1"},{"kind":"text","question":"2"},{"kind":"text","question":"3"}]`, true},
		{"unknown kind", `[{"kind":"file","question":"Q"}]`, false},
		{"unknown key", `[{"kind":"text","question":"Q","to":"x@y"}]`, false},
		{"too many", `[{"kind":"text","question":"1"},{"kind":"text","question":"2"},{"kind":"text","question":"3"},{"kind":"text","question":"4"}]`, false},
		{"html option", `[{"kind":"choice","question":"Q","options":["<a>","b"],"default":0}]`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call, err := ParseCall("c1", "ask_questions", `{"questions":`+tt.qs+`}`)
			if tt.ok != (err == nil) {
				t.Fatalf("error = %v, want ok=%v", err, tt.ok)
			}
			if err == nil && len(call.Args().Questions("questions")) == 0 {
				t.Fatal("no questions parsed")
			}
		})
	}

	call, err := ParseCall("c1", "ask_questions", `{"questions":[{"kind":"choice","question":"Q","options":["a","b"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if q := call.Args().Questions("questions")[0]; q.Default != 0 || q.Text != "Q" || len(q.Options) != 2 {
		t.Fatalf("choice without a default = %+v, want the first option preselected", q)
	}
}
