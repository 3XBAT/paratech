package toolcatalog

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const goldenV5 = "../../../../../../shared/testdata/tool_catalog_v5.json"

// TestCatalogV5: catalog v5 is v4 with a new server prompt (TASK-200): the same tools, schemas and one-of groups.
func TestCatalogV5(t *testing.T) {
	v4, v5 := goldenCatalog(t), parseFile(t, goldenV5)
	if v5.Version() != 5 || fmt.Sprint(v5.Specs()) != fmt.Sprint(v4.Specs()) || fmt.Sprint(v5.oneOf) != fmt.Sprint(v4.oneOf) {
		t.Fatalf("v5 differs from v4 beyond the version")
	}
	for i, s := range v5.Specs() {
		if w := v4.Specs()[i]; s.Name() != w.Name() || s.Schema() != w.Schema() || s.Description() != w.Description() {
			t.Fatalf("tool %s differs", s.Name())
		}
	}
}

// TestTextReply: a plain text becomes reply{text} under the exact check of a returned reply, in every catalog version;
// JSON or a call inside the text stays text.
func TestTextReply(t *testing.T) {
	for _, file := range []string{goldenV1, goldenV2, goldenV3, golden, goldenV5} {
		c := parseFile(t, file)
		for _, text := range []string{
			"Горутина — лёгкий поток выполнения в Go.",
			`{"tool":"propose_send","arguments":{"assignment":"asg-1"}}`,
			"A & B \"кавычки\" 'да' ≤ 5 €\nвторая строка",
			strings.Repeat("я", MaxTextRunes),
		} {
			call, err := c.TextReply("hub_text_1", text)
			if err != nil {
				t.Fatalf("v%d %q: %v", c.Version(), text, err)
			}
			args, err := decodeObject(call.Arguments())
			if err != nil || call.Tool() != "reply" || call.CallID() != "hub_text_1" || len(args) != 1 || args["text"] != text {
				t.Fatalf("v%d: call %v, arguments %q", c.Version(), call, call.Arguments())
			}
			if strings.Contains(call.Arguments(), `\`+"u0026") {
				t.Fatalf("arguments escape HTML: %q", call.Arguments())
			}
		}
	}
}

func TestTextReplyRefusals(t *testing.T) {
	c := goldenCatalog(t)
	for name, tc := range map[string]struct{ id, text string }{
		"empty":     {"hub_text_1", ""},
		"blank":     {"hub_text_1", " \n "},
		"too long":  {"hub_text_1", strings.Repeat("я", MaxTextRunes+1)},
		"escape":    {"hub_text_1", "a\x1b[31mb"},
		"nul":       {"hub_text_1", "a\x00b"},
		"not utf-8": {"hub_text_1", "a\xffb"},
		"bad id":    {"hub text", "ok"},
		// What the desktop's card text refuses (desktop values.Text), stricter than the general text check.
		"tab":         {"hub_text_1", "a\tb"},
		"replacement": {"hub_text_1", "a\uFFFDb"},
		"html":        {"hub_text_1", "<script>x</script>"},
		"less than":   {"hub_text_1", "a < b"},
		"greater":     {"hub_text_1", "a > b"},
		"zero width":  {"hub_text_1", "a\u200bb"},
		"bidi":        {"hub_text_1", "a\u202eb"},
		"nbsp":        {"hub_text_1", "a\u00a0b"},
		"cr":          {"hub_text_1", "a\r\nb"},
	} {
		if _, err := c.TextReply(tc.id, tc.text); !errors.Is(err, ErrInvalidCall) {
			t.Errorf("%s: err = %v, want ErrInvalidCall", name, err)
		} else if strings.Contains(err.Error(), "\x1b") || strings.Contains(err.Error(), "яяя") {
			t.Errorf("%s: the error quotes the text: %v", name, err)
		}
	}
	noReply, err := Parse([]byte(`{"catalog_version":1,"types":{},"notes":{},"tools":[` +
		`{"name":"list_subjects","class":"R","paid":false,"agent_can_execute":true,"arguments":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noReply.TextReply("hub_text_1", "ok"); !errors.Is(err, ErrInvalidCall) {
		t.Errorf("a catalog without reply: err = %v", err)
	}
}
