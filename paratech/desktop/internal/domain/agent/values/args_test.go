package values

import (
	"errors"
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
		{name: "due date", tool: "edit_due", args: `{"assignment":"a1","due":"2026-11-03"}`},
		{name: "reply text with newline", tool: "reply", args: `{"text":"Готово.\nКонспект в папке."}`},
		{name: "cannot do", tool: "cannot_do", args: `{"your_part":["Сдать лично"],"our_part":["Черновик"]}`},
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
		{name: "label too long", tool: "search", args: `{"query":"` + long + `"}`, wantErr: ErrInvalidArguments},
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
		{name: "text list too long", tool: "cannot_do", args: `{"your_part":["a","a","a","a","a","a","a","a","a"],"our_part":["b"]}`, wantErr: ErrInvalidArguments},
		{name: "too large", tool: "search", args: `{"query":"a","x":"` + strings.Repeat("x", MaxArgumentsBytes) + `"}`, wantErr: ErrInvalidArguments},
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

func TestParseCallOneOfPairs(t *testing.T) {
	tests := []struct {
		tool       string
		base, a, b string
	}{
		{tool: "make_summary", base: ``, a: `"recording":"r1"`, b: `"material":"m1"`},
		{tool: "find_homework", base: ``, a: `"recording":"r1"`, b: `"material":"m1"`},
		{tool: "triage_assignment", base: ``, a: `"assignment":"a1"`, b: `"upload":"u1"`},
		{tool: "place_upload", base: `"upload":"u1","subject":"s1"`, a: `"folder":"f1"`, b: `"lesson":"l1"`},
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
		cases := []struct {
			name string
			args string
			ok   bool
		}{
			{"first only", join(tt.base, tt.a), true},
			{"second only", join(tt.base, tt.b), true},
			{"both", join(tt.base, tt.a, tt.b), false},
			{"none", join(tt.base), false},
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
		`{"tool":"place_upload","args":{"upload":"u1","subject":"s1","folder":"f1"}},` +
		`{"tool":"make_summary","args":{"material":"m1"}},` +
		`{"tool":"create_subject","args":{"name":"Физика"}},` +
		`{"tool":"generate_draft","args":{"assignment":"a1"}},` +
		`{"tool":"create_folder","args":{"name":"Лекции"}},` +
		`{"tool":"place_upload","args":{"upload":"u2","subject":"s1"}}],"notes":["Займёт пару минут"]}`
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
		{"planned draft", "generate_draft", `{"assignment":"a1"}`, 3, true},
		{"extra argument", "generate_draft", `{"assignment":"a1","instruction":"кратко"}`, 3, false},
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
	if c, _ := ParseCall("c3", "create_folder", `{"subject":"s9","name":"Лекции"}`); !dec[4].Matches(c) {
		t.Fatal("decoded plan lost the deferred subject")
	}

	// Only an ID the step needs is deferred, and a call must fill in every deferred ID.
	lessonStep, err := DecodePlanSteps(`[{"tool":"create_folder","args":{"subject":"s1","name":"А"}},` +
		`{"tool":"place_upload","args":{"upload":"u1","subject":"s1","lesson":"l1"}}]`)
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
		{"text argument", `[{"tool":"reply","args":{"text":"x"}}]`},
		{"unknown argument", `[{"tool":"make_summary","args":{"path":"C:/x"}}]`},
		{"bad id", `[{"tool":"make_summary","args":{"material":"a/b"}}]`},
		{"duplicate key", `[{"tool":"make_summary","tool":"write_work"}]`},
		{"too many", `[` + strings.Repeat(`{"tool":"list_subjects"},`, MaxPlanSteps) + `{"tool":"list_subjects"}]`},
		{"no required argument", `[{"tool":"make_lecture_text"}]`},
		{"no due", `[{"tool":"edit_due","args":{"assignment":"a1"}}]`},
		{"no name", `[{"tool":"create_subject"}]`},
		{"neither of one-of", `[{"tool":"make_summary"}]`},
		{"both of one-of", `[{"tool":"make_summary","args":{"recording":"r1","material":"m1"}}]`},
		{"no folder or lesson", `[{"tool":"place_upload","args":{"upload":"u1","subject":"s1"}}]`},
		{"subject created later", `[{"tool":"create_folder","args":{"name":"X"}},{"tool":"create_subject","args":{"name":"Y"}}]`},
		{"folder is not a subject", `[{"tool":"create_folder","args":{"subject":"s1","name":"X"}},{"tool":"create_folder","args":{"name":"Y"}}]`},
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
		{"choice and text", `[{"kind":"choice","text":"Какой формат?","options":["docx","pdf"],"default":0},{"kind":"text","text":"Сколько страниц?"}]`, true},
		{"default out of range", `[{"kind":"choice","text":"Q","options":["a","b"],"default":2}]`, false},
		{"negative default", `[{"kind":"choice","text":"Q","options":["a","b"],"default":-1}]`, false},
		{"fractional default", `[{"kind":"choice","text":"Q","options":["a","b"],"default":0.5}]`, false},
		{"default as string", `[{"kind":"choice","text":"Q","options":["a","b"],"default":"0"}]`, false},
		{"no default", `[{"kind":"choice","text":"Q","options":["a","b"]}]`, false},
		{"one option", `[{"kind":"choice","text":"Q","options":["a"],"default":0}]`, false},
		{"text with options", `[{"kind":"text","text":"Q","options":["a","b"]}]`, false},
		{"unknown kind", `[{"kind":"file","text":"Q"}]`, false},
		{"unknown key", `[{"kind":"text","text":"Q","to":"x@y"}]`, false},
		{"too many", `[{"kind":"text","text":"1"},{"kind":"text","text":"2"},{"kind":"text","text":"3"},{"kind":"text","text":"4"}]`, false},
		{"html option", `[{"kind":"choice","text":"Q","options":["<a>","b"],"default":0}]`, false},
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
}
