package values

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
)

var wave11Tools = []Tool{
	ToolSetTeacher, ToolClearTeacher, ToolSetProfileField, ToolMoveToInbox, ToolUnlinkMaterial, ToolRemoveMaterial,
	ToolAddAlias, ToolRemoveFolder, ToolRemoveSubject,
}

// TestCatalog10SeesNoWave11Tool: catalog 11 is declared, not served. Catalog 10 (the current one) knows no wave-11
// tool, argument or setting, in a call or in a plan step; catalog 11 knows them all.
func TestCatalog10SeesNoWave11Tool(t *testing.T) {
	if CatalogVersion != CatalogWave10 || CatalogWave11 != 11 {
		t.Fatalf("CatalogVersion = %d, wave %d", CatalogVersion, CatalogWave11)
	}
	if len(ToolsAt(CatalogWave10)) != 27+len(waveTools) || !slices.Equal(Tools(), ToolsAt(CatalogWave10)) {
		t.Fatalf("catalog 10 = %v", ToolsAt(CatalogWave10))
	}
	for _, tool := range wave11Tools {
		t.Run(string(tool), func(t *testing.T) {
			for _, v := range []int{9, CatalogWave10} {
				if slices.Contains(ToolsAt(v), tool) || slices.Contains(Tools(), tool) {
					t.Fatalf("catalog %d lists the tool", v)
				}
				if _, err := ParseToolAt(string(tool), v); !errors.Is(err, ErrUnknownTool) {
					t.Fatalf("ParseToolAt(%d) = %v", v, err)
				}
				if _, err := SpecAt(tool, v); !errors.Is(err, ErrUnknownTool) {
					t.Fatalf("SpecAt(%d) = %v", v, err)
				}
				if _, err := ParseCallAt("c1", string(tool), `{}`, v); !errors.Is(err, ErrUnknownTool) {
					t.Fatalf("ParseCallAt(%d) = %v", v, err)
				}
				step := `[{"tool":"` + string(tool) + `","arguments":{"subject":"s1"}}]`
				if _, err := ParseCallAt("c1", "show_plan", `{"summary":"План","steps":`+step+`}`, v); !errors.Is(err, ErrUnknownTool) {
					t.Fatalf("planned at catalog %d: %v", v, err)
				}
			}
			if _, err := ParseTool(string(tool)); !errors.Is(err, ErrUnknownTool) {
				t.Fatalf("ParseTool = %v", err)
			}
			if got, err := ParseToolAt(string(tool), CatalogWave11); err != nil || got != tool {
				t.Fatalf("ParseToolAt(11) = %v, %v", got, err)
			}
		})
	}

	// reply.chat_title and the new settings are catalog 11 only.
	refused := []struct{ tool, args string }{
		{"reply", `{"text":"Готово","chat_title":"Матан"}`},
		{"set_setting", `{"setting":"calendar_update_on_due_change","value":"update"}`},
		{"set_setting", `{"setting":"notes_ocr","value":"local"}`},
		{"set_setting", `{"setting":"ac_power_only","value":"local"}`},
	}
	for _, r := range refused {
		// Catalog 9 has no set_setting at all.
		if _, err := ParseCallAt("c1", r.tool, r.args, 9); err == nil {
			t.Errorf("%s %s at catalog 9 parses", r.tool, r.args)
		}
		if _, err := ParseCallAt("c1", r.tool, r.args, CatalogWave10); !errors.Is(err, ErrInvalidArguments) {
			t.Errorf("%s %s at catalog 10 = %v", r.tool, r.args, err)
		}
		if _, err := ParseCall("c1", r.tool, r.args); !errors.Is(err, ErrInvalidArguments) {
			t.Errorf("%s %s at the current catalog = %v", r.tool, r.args, err)
		}
	}
	step := `[{"tool":"set_setting","arguments":{"setting":"notes_ocr","value":"local"}}]`
	if _, err := ParseCallAt("c1", "show_plan", `{"summary":"План","steps":`+step+`}`, CatalogWave10); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("notes_ocr planned at catalog 10: %v", err)
	}
	if _, err := ParseCallAt("c1", "show_plan", `{"summary":"План","steps":`+step+`}`, CatalogWave11); err != nil {
		t.Fatalf("notes_ocr planned at catalog 11: %v", err)
	}
	for name, spec := range map[string]Tool{"reply": ToolReply, "set_setting": ToolSetSetting} {
		at10, _ := SpecAt(spec, CatalogWave10)
		for _, a := range at10.Args() {
			if a.Name == "chat_title" || slices.Contains(a.Values, string(SettingNotesOCR)) ||
				slices.Contains(a.Values, string(SettingCalendarOnDueChange)) || slices.Contains(a.Values, NotesOCRLocal) {
				t.Fatalf("%s of catalog 10 has %+v", name, a)
			}
		}
	}
}

// TestCatalog11: v11 is v10 with reply and set_setting of v11, then the wave; every ID argument names a kind.
func TestCatalog11(t *testing.T) {
	want := append(slices.Clone(ToolsAt(CatalogWave10)), wave11Tools...)
	if got := ToolsAt(CatalogWave11); !slices.Equal(got, want) {
		t.Fatalf("catalog 11 = %v", got)
	}
	for _, tool := range ToolsAt(CatalogWave10) {
		at10, _ := SpecAt(tool, CatalogWave10)
		at11, err := SpecAt(tool, CatalogWave11)
		if err != nil {
			t.Fatal(err)
		}
		if tool == ToolReply || tool == ToolSetSetting {
			continue
		}
		if fmt.Sprint(at10.Args(), at10.OneOf()) != fmt.Sprint(at11.Args(), at11.OneOf()) {
			t.Fatalf("%s changed in catalog 11", tool)
		}
	}
	for _, tool := range ToolsAt(CatalogWave11) {
		spec, _ := SpecAt(tool, CatalogWave11)
		for _, a := range spec.Args() {
			if (a.Type == ArgID || a.Type == ArgIDs || a.Type == ArgCardIDs) != a.Kind.Valid() {
				t.Fatalf("%s.%s: id kind %q", tool, a.Name, a.Kind)
			}
		}
	}

	reply, _ := SpecAt(ToolReply, CatalogWave11)
	if args := reply.Args(); len(args) != 3 || args[2].Name != "chat_title" || args[2].Type != ArgChatTitle || args[2].Required {
		t.Fatalf("reply of catalog 11 = %+v", args)
	}
	set, _ := SpecAt(ToolSetSetting, CatalogWave11)
	args := set.Args()
	if !slices.Equal(args[0].Values, []string{"recording_prompts", "ac_power_only", "recognition_load",
		"calendar_update_on_due_change", "notes_ocr"}) {
		t.Fatalf("settings of catalog 11 = %v", args[0].Values)
	}
	if !slices.Equal(args[1].Values, []string{"on", "off", "gentle", "normal", "max", "ask", "update", "keep", "local"}) {
		t.Fatalf("value enum of catalog 11 = %v", args[1].Values)
	}
	if !KindSuggestion.Valid() {
		t.Fatal("suggestion is not an id kind")
	}
}

func TestWave11Settings(t *testing.T) {
	if !slices.Equal(Settings(), SettingsAt(CatalogWave11)) || len(SettingsAt(CatalogWave11)) != 5 || len(SettingsAt(CatalogWave10)) != 3 {
		t.Fatalf("settings = %v / %v", Settings(), SettingsAt(CatalogWave10))
	}
	want := map[Setting][]string{
		SettingCalendarOnDueChange: {"ask", "update", "keep"},
		SettingNotesOCR:            {"off", "local"},
	}
	for s, vs := range want {
		if !s.Valid() || !slices.Equal(SettingValues(s), vs) {
			t.Fatalf("%s values = %v", s, SettingValues(s))
		}
	}
	if ValidSettingValue(SettingNotesOCR, "cloud") || ValidSettingValue(SettingCalendarOnDueChange, "on") {
		t.Fatal("a value outside the setting is valid")
	}
}

func TestWave11Arguments(t *testing.T) {
	v200 := strings.Repeat("я", MaxProfileValueRunes)
	tests := []struct {
		name string
		tool string
		args string
		ok   bool
	}{
		{"set a lecturer", "set_teacher", `{"subject":"s1","role":"lecture","name":"Иванов И. И.","email":"ivanov@example.invalid"}`, true},
		{"set a lab teacher", "set_teacher", `{"subject":"s1","role":"lab","name":"Петрова","email":" p@x "}`, true},
		{"teacher without a name", "set_teacher", `{"subject":"s1","role":"lecture","email":"a@b"}`, false},
		{"teacher without an address", "set_teacher", `{"subject":"s1","role":"lecture","name":"Иванов"}`, false},
		{"teacher of an unknown role", "set_teacher", `{"subject":"s1","role":"seminar","name":"Иванов","email":"a@b"}`, false},
		{"teacher without a subject", "set_teacher", `{"role":"lab","name":"Иванов","email":"a@b"}`, false},
		{"address without @", "set_teacher", `{"subject":"s1","role":"lab","name":"Иванов","email":"ivanov.example"}`, false},
		{"address with two @", "set_teacher", `{"subject":"s1","role":"lab","name":"Иванов","email":"a@b@c"}`, false},
		{"address with a space", "set_teacher", `{"subject":"s1","role":"lab","name":"Иванов","email":"a b@c"}`, false},
		{"address on two lines", "set_teacher", `{"subject":"s1","role":"lab","name":"Иванов","email":"a@b\nc"}`, false},
		{"address too long", "set_teacher", `{"subject":"s1","role":"lab","name":"Иванов","email":"a@` + strings.Repeat("b", MaxEmailBytes) + `"}`, false},
		{"name on two lines", "set_teacher", `{"subject":"s1","role":"lab","name":"a\nb","email":"a@b"}`, false},
		{"teacher with a link", "set_teacher", `{"subject":"s1","role":"lab","name":"x","email":"a@b","link":"https://x"}`, false},
		{"clear a teacher", "clear_teacher", `{"subject":"s1","role":"practice"}`, true},
		{"clear without a role", "clear_teacher", `{"subject":"s1"}`, false},
		{"clear with a name", "clear_teacher", `{"subject":"s1","role":"lab","name":"x"}`, false},

		{"group", "set_profile_field", `{"field":"group","value":"ИВТ-21"}`, true},
		{"full name of 200", "set_profile_field", `{"field":"full_name","value":"` + v200 + `"}`, true},
		{"value of 201", "set_profile_field", `{"field":"city","value":"` + v200 + `я"}`, false},
		{"blank value", "set_profile_field", `{"field":"city","value":"  "}`, false},
		{"value with a bidi mark", "set_profile_field", `{"field":"city","value":"a\u202eb"}`, false},
		{"value on two lines", "set_profile_field", `{"field":"city","value":"a\nb"}`, false},
		{"unknown field", "set_profile_field", `{"field":"hub_key","value":"x"}`, false},
		{"no value", "set_profile_field", `{"field":"city"}`, false},
		{"value as a number", "set_profile_field", `{"field":"city","value":5}`, false},

		{"to the inbox", "move_to_inbox", `{"material":"m1"}`, true},
		{"to the inbox without material", "move_to_inbox", `{}`, false},
		{"unlink", "unlink_material", `{"material":"m1","lesson":"l1"}`, true},
		{"unlink without lesson", "unlink_material", `{"material":"m1"}`, false},
		{"remove a material", "remove_material", `{"material":"m1"}`, true},
		{"remove a material by path", "remove_material", `{"material":"m1","path":"C:/x"}`, false},
		{"add an alias", "add_alias", `{"subject":"s1","suggestion":"g-1"}`, true},
		{"add an alias as text", "add_alias", `{"subject":"s1","alias":"Матан"}`, false},
		{"add an alias without suggestion", "add_alias", `{"subject":"s1"}`, false},
		{"suggestion id of another shape", "add_alias", `{"subject":"s1","suggestion":"Мат ан"}`, false},
		{"remove a folder", "remove_folder", `{"subject":"s1","folder":"f1"}`, true},
		{"remove a folder without subject", "remove_folder", `{"folder":"f1"}`, false},
		{"remove a subject", "remove_subject", `{"subject":"s1"}`, true},
		{"remove a subject with its name", "remove_subject", `{"subject":"s1","name":"Матан"}`, false},

		{"reply with a title", "reply", `{"text":"Готово","chat_title":"Матанализ: ряды"}`, true},
		{"reply with a bad title is still a reply", "reply", `{"text":"Готово","chat_title":"a\u202eb"}`, true},
		{"reply with a title as a number", "reply", `{"text":"Готово","chat_title":5}`, true},
		{"reply with a null title", "reply", `{"text":"Готово","chat_title":null}`, true},
		{"reply without text", "reply", `{"chat_title":"Матан"}`, false},

		{"calendar update", "set_setting", `{"setting":"calendar_update_on_due_change","value":"update"}`, true},
		{"calendar ask", "set_setting", `{"setting":"calendar_update_on_due_change","value":"ask"}`, true},
		{"calendar on", "set_setting", `{"setting":"calendar_update_on_due_change","value":"on"}`, false},
		{"ocr local", "set_setting", `{"setting":"notes_ocr","value":"local"}`, true},
		{"ocr off", "set_setting", `{"setting":"notes_ocr","value":"off"}`, true},
		{"ocr cloud is the page's only", "set_setting", `{"setting":"notes_ocr","value":"cloud"}`, false},
		{"ocr keep", "set_setting", `{"setting":"notes_ocr","value":"keep"}`, false},
		{"toggle local", "set_setting", `{"setting":"ac_power_only","value":"local"}`, false},
		{"v10 setting still works", "set_setting", `{"setting":"recognition_load","value":"max"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCallAt("c1", tt.tool, tt.args, CatalogWave11)
			if tt.ok != (err == nil) {
				t.Fatalf("ParseCallAt = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidArguments) {
				t.Fatalf("error = %v, want ErrInvalidArguments", err)
			}
		})
	}
}

func TestWave11ArgumentValues(t *testing.T) {
	c, err := ParseCallAt("c1", "set_teacher", `{"subject":"s1","role":"lab","name":" Петрова ","email":" p@example.invalid "}`, CatalogWave11)
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := c.Args().Email("email"); !ok || string(e) != "p@example.invalid" {
		t.Fatalf("email = %q, %v", string(e), ok)
	}
	if n, ok := c.Args().Label("name"); !ok || n != "Петрова" {
		t.Fatalf("name = %q", n)
	}
	if refs := c.Refs(); len(refs) != 1 || refs[0].Kind() != KindSubject {
		t.Fatalf("refs = %v", refs)
	}

	p, err := ParseCallAt("c1", "set_profile_field", `{"field":"university","value":" МГУ "}`, CatalogWave11)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := p.Args().ProfileValue("value"); !ok || string(v) != "МГУ" {
		t.Fatalf("value = %q", string(v))
	}
	if _, ok := p.Args().Label("value"); ok {
		t.Fatal("a profile value reads as a label")
	}

	a, err := ParseCallAt("c1", "add_alias", `{"subject":"s1","suggestion":"g-1"}`, CatalogWave11)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := a.Args().ID("suggestion"); !ok || r.Kind() != KindSuggestion || len(a.Refs()) != 2 {
		t.Fatalf("suggestion = %v (goes through the provenance check)", r)
	}

	r, err := ParseCallAt("c1", "reply", `{"text":"Готово","chat_title":"  Матан:\n ряды  "}`, CatalogWave11)
	if err != nil {
		t.Fatal(err)
	}
	if title, ok := r.Args().ChatTitle("chat_title"); !ok || title != "Матан: ряды" {
		t.Fatalf("chat_title = %q, %v", title, ok)
	}
	bad, err := ParseCallAt("c1", "reply", `{"text":"Готово","chat_title":"<b>x</b>"}`, CatalogWave11)
	if err != nil || bad.Args().Has("chat_title") {
		t.Fatalf("a bad title is kept: %v", err)
	}
	if text, _ := bad.Args().Text("text"); text != "Готово" {
		t.Fatalf("text = %q", text)
	}
}

func TestWave11ValueParsers(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	tests := []struct {
		name  string
		parse func(string) (string, error)
		in    string
		want  string // "" refuses
	}{
		{"email", fn(ParseEmail), "  ivanov@example.invalid ", "ivanov@example.invalid"},
		{"email at the limit", fn(ParseEmail), "a@" + long(MaxEmailBytes-2), "a@" + long(MaxEmailBytes-2)},
		{"email over the limit", fn(ParseEmail), "a@" + long(MaxEmailBytes-1), ""},
		{"email empty", fn(ParseEmail), "", ""},
		{"email without local part", fn(ParseEmail), "@b", ""},
		{"email without domain", fn(ParseEmail), "a@", ""},
		{"email with a tab", fn(ParseEmail), "a\t@b", ""},
		{"email with a zero-width space", fn(ParseEmail), "a\u200b@b", ""},
		{"email with a no-break space", fn(ParseEmail), "a\u00a0@b", ""},

		{"link", fn(ParseCallLink), " https://telemost.example.org/j/123 ", "https://telemost.example.org/j/123"},
		{"link at the limit", fn(ParseCallLink), long(MaxCallLinkBytes), long(MaxCallLinkBytes)},
		{"link over the limit", fn(ParseCallLink), long(MaxCallLinkBytes + 1), ""},
		{"link empty", fn(ParseCallLink), "  ", ""},
		{"link with a space", fn(ParseCallLink), "https://x/a b", ""},
		{"link on two lines", fn(ParseCallLink), "https://x/a\nb", ""},
		{"link with a bidi override", fn(ParseCallLink), "https://x/\u202ea", ""},
		{"link with a bell", fn(ParseCallLink), "https://x/\u0007", ""},

		{"profile value", fn(ParseProfileValue), " ИВТ-21 ", "ИВТ-21"},
		{"profile value with angles", fn(ParseProfileValue), "a <b>", "a <b>"},
		{"profile value with a line separator", fn(ParseProfileValue), "a\u2028b", ""},
		{"profile value with a zero-width joiner", fn(ParseProfileValue), "a\u200db", ""},

		{"title", fn(ParseChatTitle), "Ряды\tТейлора\n", "Ряды Тейлора"},
		{"title of 60", fn(ParseChatTitle), strings.Repeat("я", MaxChatTitleRunes), strings.Repeat("я", MaxChatTitleRunes)},
		{"title of 61", fn(ParseChatTitle), strings.Repeat("я", MaxChatTitleRunes+1), ""},
		{"title of spaces", fn(ParseChatTitle), " \n ", ""},
		{"title with a bidi mark", fn(ParseChatTitle), "a\u200fb", ""},
		{"title with HTML", fn(ParseChatTitle), "<i>x</i>", ""},
		{"title with a control", fn(ParseChatTitle), "a\u0000b", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.parse(tt.in)
			if tt.want == "" {
				if !errors.Is(err, ErrInvalidText) {
					t.Fatalf("parse = %q, %v; want a refusal", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("parse = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func fn[T ~string](parse func(string) (T, error)) func(string) (string, error) {
	return func(s string) (string, error) {
		v, err := parse(s)
		return string(v), err
	}
}

// TestCallLinkArgument: call_link is declared for the rows of set_call_links (TASK-434); no tool takes it yet, so a
// schema of this test parses it the way a tool will.
func TestCallLinkArgument(t *testing.T) {
	spec := ToolSpec{tool: "rows_under_test", args: []ArgSpec{typed("link", ArgCallLink, true)}}
	args, err := parseArgs(spec, []byte(`{"link":" https://telemost.example.org/j/1 "}`), CatalogWave11)
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := args.CallLink("link"); !ok || string(l) != "https://telemost.example.org/j/1" {
		t.Fatalf("link = %q", string(l))
	}
	for _, raw := range []string{`{"link":"a b"}`, `{"link":""}`, `{"link":1}`, `{"link":"` + strings.Repeat("a", MaxCallLinkBytes+1) + `"}`} {
		if _, err := parseArgs(spec, []byte(raw), CatalogWave11); !errors.Is(err, ErrInvalidArguments) {
			t.Errorf("%.40s = %v", raw, err)
		}
	}
}

// TestWave11ValuesAreNotEchoed: a value the model typed (an address, a link, a profile value) never shows in an
// error, in %v of the call or its arguments, or in a log line.
func TestWave11ValuesAreNotEchoed(t *testing.T) {
	const secret = "ivanov.secret"
	for _, raw := range []struct{ tool, args string }{
		{"set_teacher", `{"subject":"s1","role":"lab","name":"Иванов","email":"` + secret + `"}`},
		{"set_teacher", `{"subject":"s1","role":"lab","name":"Иванов","email":"` + secret + `@a@b"}`},
		{"set_profile_field", `{"field":"city","value":"` + secret + "\u2028x" + `"}`},
		{"set_profile_field", `{"field":"city","value":"` + secret + strings.Repeat("я", MaxProfileValueRunes) + `"}`},
	} {
		_, err := ParseCallAt("c1", raw.tool, raw.args, CatalogWave11)
		if !errors.Is(err, ErrInvalidArguments) || strings.Contains(err.Error(), secret) {
			t.Fatalf("error = %v", err)
		}
	}
	if _, err := ParseCallLink(secret + " x"); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("link error = %v", err)
	}

	c, err := ParseCallAt("c1", "set_teacher", `{"subject":"s1","role":"lab","name":"Иванов","email":"`+secret+`@example.invalid"}`, CatalogWave11)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseCallAt("c2", "set_profile_field", `{"field":"city","value":"`+secret+`"}`, CatalogWave11)
	if err != nil {
		t.Fatal(err)
	}
	email, _ := c.Args().Email("email")
	value, _ := p.Args().ProfileValue("value")
	link, _ := ParseCallLink("https://" + secret)
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("call", "call", c, "args", p.Args(), "email", email, "value", value, "link", link)
	text := slog.New(slog.NewTextHandler(&buf, nil))
	text.Info("call", "call", c, "args", p.Args(), "email", email, "value", value, "link", link)
	for _, s := range []string{
		fmt.Sprint(c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c.Args()), fmt.Sprintf("%v", p),
		fmt.Sprintf("%s %v %+v %q", email, value, link, email), buf.String(),
	} {
		if strings.Contains(s, secret) || strings.Contains(s, "Иванов") {
			t.Fatalf("a value shows: %s", s)
		}
	}
	if got := fmt.Sprint(c); got != "call(c1 set_teacher args(email,name,role,subject))" {
		t.Fatalf("call prints %q", got)
	}
}

// TestWave11PlanSteps: the library tools and clear_teacher plan like any ID tool; a value the model types is never a
// plan-step argument, so set_teacher and set_profile_field are not planned (their «Да» card shows the value).
func TestWave11PlanSteps(t *testing.T) {
	steps := `[` +
		`{"tool":"move_to_inbox","arguments":{"material":"m1"}},` +
		`{"tool":"unlink_material","arguments":{"material":"m1","lesson":"l1"}},` +
		`{"tool":"create_subject","arguments":{"name":"Матан"}},` +
		`{"tool":"add_alias","arguments":{"suggestion":"g-1"}},` +
		`{"tool":"remove_folder","arguments":{"subject":"s1","folder":"f1"}},` +
		`{"tool":"clear_teacher","arguments":{"subject":"s1","role":"lab"}},` +
		`{"tool":"set_setting","arguments":{"setting":"calendar_update_on_due_change","value":"keep"}}` +
		`]`
	c, err := ParseCallAt("c1", "show_plan", `{"summary":"План","steps":`+steps+`}`, CatalogWave11)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Args().PlanSteps("steps")
	if len(got) != 7 || !slices.Equal(got[3].Deferred(), []string{"subject"}) {
		t.Fatalf("steps = %v", got)
	}
	alias, _ := ParseCallAt("c2", "add_alias", `{"subject":"s9","suggestion":"g-1"}`, CatalogWave11)
	other, _ := ParseCallAt("c3", "add_alias", `{"subject":"s9","suggestion":"g-2"}`, CatalogWave11)
	if !got[3].Matches(alias) || got[3].Matches(other) {
		t.Fatal("an alias step matched wrongly")
	}
	enc, err := EncodePlanSteps(got)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := decodePlanSteps([]byte(enc), true, CatalogWave11); err != nil || len(back) != 7 {
		t.Fatalf("stored wave-11 plan = %v, %v", back, err)
	}

	for _, b := range []string{
		`[{"tool":"set_teacher","arguments":{"subject":"s1","role":"lab","name":"x"}}]`,
		`[{"tool":"set_teacher","arguments":{"subject":"s1","role":"lab","name":"x","email":"a@b"}}]`,
		`[{"tool":"set_profile_field","arguments":{"field":"city","value":"x"}}]`,
		`[{"tool":"set_setting","arguments":{"setting":"notes_ocr","value":"cloud"}}]`,
	} {
		if _, err := ParseCallAt("c1", "show_plan", `{"summary":"План","steps":`+b+`}`, CatalogWave11); !errors.Is(err, ErrInvalidArguments) {
			t.Errorf("plan %s = %v", b, err)
		}
	}
}

// TestStoredDataOfCatalog10UnderCatalog11: catalog 11 is additive, so a plan and calls stored under catalog 10 decode
// under it unchanged (what happens when TASK-443 switches CatalogVersion).
func TestStoredDataOfCatalog10UnderCatalog11(t *testing.T) {
	stored := `[{"tool":"set_setting","arguments":{"setting":"recognition_load","value":"max"}},{"tool":"note","arguments":{"action":"remove","note":"n1"}}]`
	for _, cat := range []int{CatalogWave10, CatalogWave11} {
		steps, err := decodePlanSteps([]byte(stored), true, cat)
		if err != nil || len(steps) != 2 {
			t.Fatalf("catalog %d: stored v10 plan = %v, %v", cat, steps, err)
		}
		if enc, err := EncodePlanSteps(steps); err != nil || enc != stored {
			t.Fatalf("catalog %d: EncodePlanSteps = %s, %v", cat, enc, err)
		}
	}
	for _, raw := range []struct{ tool, args string }{
		{"reply", `{"text":"Готово","assignments":["a1"]}`},
		{"set_setting", `{"setting":"ac_power_only","value":"on"}`},
		{"change_assignment", `{"assignment":"a1","action":"delete"}`},
	} {
		at10, err10 := ParseCallAt("c1", raw.tool, raw.args, CatalogWave10)
		at11, err11 := ParseCallAt("c1", raw.tool, raw.args, CatalogWave11)
		if err10 != nil || err11 != nil || fmt.Sprint(at10.Args().Names()) != fmt.Sprint(at11.Args().Names()) {
			t.Fatalf("%s: %v / %v", raw.tool, err10, err11)
		}
	}
}
