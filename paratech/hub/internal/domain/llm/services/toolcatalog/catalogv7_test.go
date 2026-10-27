package toolcatalog

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"paratech/hub/internal/domain/llm/values"
)

const goldenV7 = "../../../../../../shared/testdata/tool_catalog_v7.json"

// TestCatalogV7 (TASK-223): v7 is v6 with reply.text a reply_text of up to MaxReplyTextRunes characters; every other
// tool, and every other card text, keeps its v6 schema.
func TestCatalogV7(t *testing.T) {
	v6, v7 := parseFile(t, goldenV6), parseFile(t, goldenV7)
	if v7.Version() != 7 || len(v7.Specs()) != len(v6.Specs()) || strings.Join(v7.order, ",") != strings.Join(v6.order, ",") {
		t.Fatalf("v7: version %d, %d tools", v7.Version(), len(v7.Specs()))
	}
	if !v7.longReply() || v6.longReply() {
		t.Fatalf("longReply: v7 %v, v6 %v", v7.longReply(), v6.longReply())
	}
	for _, old := range v6.Specs() {
		got := specFor(t, v7, old.Name())
		if got.Description() != old.Description() {
			t.Errorf("%s: description changed", old.Name())
		}
		if old.Name() != toolReply && got.Schema() != old.Schema() {
			t.Errorf("%s changed:\nv6 %s\nv7 %s", old.Name(), old.Schema(), got.Schema())
		}
	}

	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal([]byte(specFor(t, v7, toolReply).Schema()), &schema); err != nil {
		t.Fatal(err)
	}
	text := schema.Properties["text"]
	if text["maxLength"] != float64(MaxReplyTextRunes) || text["minLength"] != float64(1) ||
		!strings.Contains(text["description"].(string), "at most 4000 characters") || len(schema.Properties) != 2 {
		t.Fatalf("reply schema = %+v", schema)
	}
	if MaxReplyTextRunes != 4000 || MaxTextRunes != 300 {
		t.Fatalf("limits %d and %d", MaxReplyTextRunes, MaxTextRunes)
	}
}

// TestReplyTextCheck: a returned reply of v7 takes up to 4000 characters with line breaks and angle brackets; the other
// card texts of v7 stay at 300, and v6 keeps 300 for reply too.
func TestReplyTextCheck(t *testing.T) {
	v6, v7 := parseFile(t, goldenV6), parseFile(t, goldenV7)
	reply := func(text string) values.ToolCall {
		b, err := json.Marshal(map[string]any{"text": text, "assignments": []string{"asg-1"}})
		if err != nil {
			t.Fatal(err)
		}
		return call(t, "reply", string(b))
	}
	for name, text := range map[string]string{
		"limit":      strings.Repeat("я", MaxReplyTextRunes),
		"301":        strings.Repeat("я", MaxTextRunes+1),
		"paragraphs": "Первый абзац.\n\nВторой абзац.",
		"angles":     "Если a < b и b > c, то <ничего> не следует.",
	} {
		if err := v7.Check(reply(text)); err != nil {
			t.Errorf("v7 %s: %v", name, err)
		}
	}
	for name, tc := range map[string]struct {
		c    *Catalog
		call values.ToolCall
	}{
		"v7 over the limit":     {v7, reply(strings.Repeat("я", MaxReplyTextRunes+1))},
		"v7 blank":              {v7, reply(" \n ")},
		"v7 control":            {v7, reply("a\x1bb")},
		"v6 reply over 300":     {v6, reply(strings.Repeat("я", MaxTextRunes+1))},
		"v7 plan summary 301":   {v7, call(t, "show_plan", `{"summary":"`+strings.Repeat("я", MaxTextRunes+1)+`","steps":[{"tool":"list_subjects"}]}`)},
		"v7 cannot_do item 301": {v7, call(t, "cannot_do", `{"your_part":["`+strings.Repeat("я", MaxTextRunes+1)+`"],"our_part":[]}`)},
		"v7 question 301":       {v7, call(t, "ask_questions", `{"questions":[{"kind":"text","question":"`+strings.Repeat("я", MaxTextRunes+1)+`"}]}`)},
	} {
		if err := tc.c.Check(tc.call); !errors.Is(err, ErrInvalidCall) {
			t.Errorf("%s: err = %v, want ErrInvalidCall", name, err)
		}
	}
}

// TestReplyTextOnlyInCatalog7: reply_text before catalog 7, on another tool or argument, or on a reading tool is a
// catalog error; every released catalog still parses.
func TestReplyTextOnlyInCatalog7(t *testing.T) {
	raw, err := os.ReadFile(goldenV7)
	if err != nil {
		t.Fatal(err)
	}
	v7 := string(raw)
	for name, text := range map[string]string{
		"v7 marked 6":    strings.Replace(v7, `"catalog_version": 7,`, `"catalog_version": 6,`, 1),
		"v7 marked 1":    strings.Replace(v7, `"catalog_version": 7,`, `"catalog_version": 1,`, 1),
		"plan summary":   strings.Replace(v7, `{"name": "summary", "type": "text", "required": true}`, `{"name": "summary", "type": "reply_text", "required": true}`, 1),
		"search query":   strings.Replace(v7, `{"name": "query", "type": "query", "required": true}`, `{"name": "query", "type": "reply_text", "required": true}`, 1),
		"with values":    strings.Replace(v7, `{"name": "text", "type": "reply_text", "required": true}`, `{"name": "text", "type": "reply_text", "required": true, "values": ["a"]}`, 1),
		"reply.assigned": strings.Replace(v7, `{"name": "assignments", "type": "card_ids", "required": false}`, `{"name": "assignments", "type": "reply_text", "required": false}`, 1),
	} {
		if text == v7 {
			t.Fatalf("%s: the mutation did not apply", name)
		}
		if _, err := Parse([]byte(text)); !errors.Is(err, ErrInvalidCatalog) {
			t.Errorf("%s: err = %v, want ErrInvalidCatalog", name, err)
		}
	}
	for _, file := range []string{goldenV1, goldenV2, goldenV3, golden, goldenV5, goldenV6, goldenV7} {
		parseFile(t, file)
	}
}

// replyText reads reply.text of a call and checks the call has nothing else but the given extra arguments.
func replyText(t *testing.T, c values.ToolCall, extra int) string {
	t.Helper()
	args, err := decodeObject(c.Arguments())
	if err != nil || c.Tool() != toolReply || len(args) != 1+extra {
		t.Fatalf("call %v: %v", c, err)
	}
	s, ok := args[argReplyText].(string)
	if !ok {
		t.Fatalf("text = %v", args[argReplyText])
	}
	return s
}

// TestTextReplyV7: a plain text of up to 4000 characters becomes the reply as it is, angle brackets included and not
// escaped; a longer one is cut at a sentence with " …", never refused and never split inside a character.
func TestTextReplyV7(t *testing.T) {
	c := parseFile(t, goldenV7)
	sentence := "Преобразование Фурье раскладывает сигнал на частоты. " // 53 runes
	long := strings.Repeat(sentence, 100)                               // 5300 runes, about 9.8 KB
	exact := strings.Repeat("я", MaxReplyTextRunes)

	for name, text := range map[string]string{
		"short":      "Горутина — лёгкий поток выполнения в Go.",
		"exact":      exact,
		"paragraphs": "Первый абзац.\n\nВторой абзац:\n1. шаг;\n2. шаг.",
		"angles":     "Если a < b, то b > a. <b>не HTML</b>",
	} {
		call, err := c.TextReply("hub_text_1", text)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := replyText(t, call, 0); got != text {
			t.Fatalf("%s: text changed: %q", name, got)
		}
		if strings.Contains(call.Arguments(), `\`+"u003c") || strings.Contains(call.Arguments(), `\`+"u003e") {
			t.Fatalf("%s: arguments escape angle brackets: %q", name, call.Arguments())
		}
	}

	call, err := c.TextReply("hub_text_1", long)
	if err != nil {
		t.Fatalf("long: %v", err)
	}
	got := replyText(t, call, 0)
	n := utf8.RuneCountInString(got)
	if n > MaxReplyTextRunes || n < MaxReplyTextRunes/2 || !utf8.ValidString(got) || !strings.HasSuffix(got, ". …") ||
		!strings.HasPrefix(long, strings.TrimSuffix(got, " …")) || len(call.Arguments()) > values.MaxArgumentsBytes {
		t.Fatalf("long: %d runes, %d bytes, ends %q", n, len(call.Arguments()), got[len(got)-20:])
	}
	if err := c.CheckReturned([]values.ToolCall{call}); err != nil {
		t.Fatalf("the cut reply fails its own check: %v", err)
	}

	// One over the limit without any sentence end is cut at a space.
	words := strings.Repeat("слово ", MaxReplyTextRunes/6+1)
	call, err = c.TextReply("hub_text_1", strings.TrimSpace(words)+" конец")
	if err != nil {
		t.Fatal(err)
	}
	if got := replyText(t, call, 0); !strings.HasSuffix(got, "слово…") || utf8.RuneCountInString(got) > MaxReplyTextRunes {
		t.Fatalf("words: ends %q", got[len(got)-20:])
	}

	// Three-byte characters over the byte budget of the arguments are cut further, still at a character.
	wide := strings.Repeat("漢字。 ", MaxReplyTextRunes/4)
	call, err = c.TextReply("hub_text_1", strings.TrimSpace(wide))
	if err != nil {
		t.Fatalf("wide: %v", err)
	}
	if got := replyText(t, call, 0); !utf8.ValidString(got) || len(call.Arguments()) > values.MaxArgumentsBytes || !strings.HasSuffix(got, "…") {
		t.Fatalf("wide: %d bytes", len(call.Arguments()))
	}

	for name, text := range map[string]string{
		"tab":    "a\tb",
		"escape": "a\x1bb",
		"bidi":   "a" + string(rune(0x202e)) + "b",
		"blank":  " \n ",
	} {
		if _, err := c.TextReply("hub_text_1", text); !errors.Is(err, ErrInvalidCall) {
			t.Errorf("%s: err = %v, want ErrInvalidCall", name, err)
		}
	}
}

// TestTextReplyOldCatalogsUnchanged: before catalog 7 a text over 300 characters or with an angle bracket is still the
// old refusal, never cut.
func TestTextReplyOldCatalogsUnchanged(t *testing.T) {
	for _, file := range []string{goldenV1, goldenV3, golden, goldenV5, goldenV6} {
		c := parseFile(t, file)
		for name, text := range map[string]string{
			"301":  strings.Repeat("Да. ", 76),
			"less": "a < b",
			"more": "a > b",
		} {
			if _, err := c.TextReply("hub_text_1", text); !errors.Is(err, ErrInvalidCall) {
				t.Errorf("v%d %s: err = %v, want ErrInvalidCall", c.Version(), name, err)
			}
		}
	}
}

// TestFitReturned: a returned v7 reply over the limit is cut and keeps its other arguments and id; a reply within the
// limit, another tool, a reply that does not decode and every call of v6 stay as they are.
func TestFitReturned(t *testing.T) {
	v6, v7 := parseFile(t, goldenV6), parseFile(t, goldenV7)
	over := strings.Repeat("Ответ длинный. ", 300) // 4500 runes, ASCII spaces: within the 8 KiB of the wire
	overArgs, err := encodeObject(map[string]any{"text": over, "assignments": []any{"asg-1", "asg-2"}})
	if err != nil {
		t.Fatal(err)
	}
	calls := []values.ToolCall{
		call(t, "list_subjects", `{}`),
		mustCall(t, "toolu_2", "reply", overArgs),
		mustCall(t, "toolu_3", "reply", `{"text":"Коротко."}`),
		mustCall(t, "toolu_4", "reply", `{"text":5}`),
	}

	got, cut := v7.FitReturned(calls)
	if !cut || len(got) != len(calls) || got[0] != calls[0] || got[2] != calls[2] || got[3] != calls[3] {
		t.Fatalf("FitReturned = %v, %v", got, cut)
	}
	if got[1].CallID() != "toolu_2" || got[1].Tool() != "reply" {
		t.Fatalf("cut call = %v", got[1])
	}
	text := replyText(t, got[1], 1)
	if utf8.RuneCountInString(text) > MaxReplyTextRunes || !strings.HasSuffix(text, "длинный. …") ||
		!strings.Contains(got[1].Arguments(), `"assignments":["asg-1","asg-2"]`) {
		t.Fatalf("cut call arguments = %q", got[1].Arguments())
	}
	if err := v7.CheckReturned(got[1:3]); err != nil {
		t.Fatalf("the cut calls fail the check: %v", err)
	}
	if &got[0] == &calls[0] {
		t.Fatal("FitReturned shares the input slice")
	}

	same, cut := v6.FitReturned(calls)
	if cut || len(same) != len(calls) || same[1] != calls[1] {
		t.Fatalf("v6 changed the calls")
	}
	if err := v6.CheckReturned(same[1:2]); !errors.Is(err, ErrInvalidCall) {
		t.Fatalf("v6 long reply: err = %v, want ErrInvalidCall", err)
	}
}

// TestFitReturnedOverEightKiB (TASK-223): a v7 reply whose arguments are over 8 KiB, by its length in Cyrillic or by
// three-byte characters within 4000 runes, is cut under 8 KiB and passes CheckReturned. Under v6 it is left as it is
// and refused; a call of another tool over 8 KiB is refused under v7 even when its schema holds.
func TestFitReturnedOverEightKiB(t *testing.T) {
	v6, v7 := parseFile(t, goldenV6), parseFile(t, goldenV7)
	returned := func(tool string, v map[string]any) values.ToolCall {
		t.Helper()
		args, err := encodeObject(v)
		if err != nil {
			t.Fatal(err)
		}
		if len(args) <= values.MaxArgumentsBytes {
			t.Fatalf("fixture of %d bytes is within 8 KiB", len(args))
		}
		c, err := values.NewReturnedToolCall("toolu_1", tool, args)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	for name, text := range map[string]string{
		"cyrillic": strings.Repeat("Преобразование Фурье раскладывает сигнал на частоты. ", 114), // ~6000 runes
		"wide":     strings.TrimSpace(strings.Repeat("漢字。 ", MaxReplyTextRunes/4)),               // 3999 runes, ~10 KB
	} {
		calls := []values.ToolCall{returned("reply", map[string]any{"text": text})}
		got, cut := v7.FitReturned(calls)
		if !cut || len(got[0].Arguments()) > values.MaxArgumentsBytes || !strings.HasSuffix(replyText(t, got[0], 0), "…") {
			t.Fatalf("%s: cut %t, %d bytes", name, cut, len(got[0].Arguments()))
		}
		if err := v7.CheckReturned(got); err != nil {
			t.Fatalf("%s: the cut reply fails the check: %v", name, err)
		}
		same, cut := v6.FitReturned(calls)
		if cut || same[0] != calls[0] {
			t.Fatalf("%s: v6 changed the call", name)
		}
		if err := v6.CheckReturned(same); !errors.Is(err, ErrInvalidCall) {
			t.Fatalf("%s: v6: err = %v, want ErrInvalidCall", name, err)
		}
	}

	items := make([]any, 0, 10)
	for range 10 {
		items = append(items, strings.Repeat("ш", MaxTextRunes))
	}
	other := returned("cannot_do", map[string]any{"your_part": items, "our_part": items})
	if err := v7.Check(other); err != nil {
		t.Fatalf("the cannot_do fixture breaks its schema: %v", err)
	}
	got, cut := v7.FitReturned([]values.ToolCall{other})
	if cut || got[0] != other {
		t.Fatal("FitReturned changed a cannot_do call")
	}
	if err := v7.CheckReturned(got); !errors.Is(err, ErrInvalidCall) {
		t.Fatalf("cannot_do over 8 KiB: err = %v, want ErrInvalidCall", err)
	}
}

func mustCall(t *testing.T, id, tool, args string) values.ToolCall {
	t.Helper()
	c, err := values.NewToolCall(id, tool, args)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCutAtSentence(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		max  int
		want string
	}{
		"within":     {"Короткий текст.", 20, "Короткий текст."},
		"exact":      {"абвгд", 5, "абвгд"},
		"sentence":   {"Первое предложение. Второе предложение длинное.", 30, "Первое предложение. …"},
		"question":   {"Что это? Это ответ на вопрос.", 15, "Что это? …"},
		"line break": {"Первая строка\nВторая строка тут", 20, "Первая строка…"},
		"ellipsis":   {"Так… и дальше много текста", 8, "Так…"},
		"space":      {"слово слово слово слово", 15, "слово слово…"},
		"hard":       {"ааааааааааааааааааааа", 10, "аааааааа…"},
		// A sentence end in the first half of the room is too early: the cut goes to the last space instead.
		"early sentence": {"Да. ббббббббб ввввввввв гггггггг", 24, "Да. ббббббббб…"},
		"no dot inside":  {"v1.2 версия обновлена сегодня", 14, "v1.2 версия…"},
	} {
		got := cutAtSentence(tc.in, tc.max)
		if got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
		if utf8.RuneCountInString(got) > tc.max || !utf8.ValidString(got) {
			t.Errorf("%s: %q is over %d characters", name, got, tc.max)
		}
	}
}
