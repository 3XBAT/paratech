package values

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNewSpec(t *testing.T) {
	s, err := NewSpec("job-1:summary", "summary", "default", "Summarize.", "lesson text", 512)
	if err != nil {
		t.Fatal(err)
	}
	if s.Key() != "job-1:summary" || s.Operation() != OperationSummary || s.Alias() != "default" || s.Instructions() != "Summarize." ||
		s.Data() != "lesson text" || s.MaxOutput() != 512 || s.PayloadBytes() != 21 {
		t.Fatalf("got %v", s)
	}
	for _, ok := range []struct{ key, op, alias, instr, data string }{
		{key: strings.Repeat("~", 128), op: "homework_draft", alias: "a", instr: "i"},
		{key: "!", op: "extract_homework_deadlines", alias: "0-_" + strings.Repeat("z", 61), instr: "i", data: ""},
		{key: "k", op: "summary", alias: "a", instr: strings.Repeat("i", MaxPayloadBytes-2), data: "д"},
		{key: "k", op: "detailed_notes", alias: "a", instr: "i", data: "lesson"},
	} {
		if _, err := NewSpec(ok.key, ok.op, ok.alias, ok.instr, ok.data, 0); err != nil {
			t.Errorf("NewSpec(%q, %q, %q): %v", ok.key, ok.op, ok.alias, err)
		}
	}
}

func TestNewSpecRejects(t *testing.T) {
	tests := []struct {
		name                        string
		key, op, alias, instr, data string
		maxOutput                   int64
	}{
		{name: "empty key", op: "summary", alias: "a", instr: "i"},
		{name: "long key", key: strings.Repeat("k", 129), op: "summary", alias: "a", instr: "i"},
		{name: "key with space", key: "a b", op: "summary", alias: "a", instr: "i"},
		{name: "key with control", key: "a\tb", op: "summary", alias: "a", instr: "i"},
		{name: "key not ASCII", key: "ключ", op: "summary", alias: "a", instr: "i"},
		{name: "unspecified operation", key: "k", op: "", alias: "a", instr: "i"},
		{name: "unknown operation", key: "k", op: "translate", alias: "a", instr: "i"},
		{name: "empty alias", key: "k", op: "summary", instr: "i"},
		{name: "alias upper case", key: "k", op: "summary", alias: "Default", instr: "i"},
		{name: "alias leading dash", key: "k", op: "summary", alias: "-a", instr: "i"},
		{name: "alias with dot", key: "k", op: "summary", alias: "a.b", instr: "i"},
		{name: "long alias", key: "k", op: "summary", alias: strings.Repeat("a", 65), instr: "i"},
		{name: "empty instructions", key: "k", op: "summary", alias: "a"},
		{name: "instructions with NUL", key: "k", op: "summary", alias: "a", instr: "a\x00"},
		{name: "instructions not UTF-8", key: "k", op: "summary", alias: "a", instr: "\xff"},
		{name: "data with NUL", key: "k", op: "summary", alias: "a", instr: "i", data: "\x00"},
		{name: "data not UTF-8", key: "k", op: "summary", alias: "a", instr: "i", data: "\xc3"},
		{name: "negative max output", key: "k", op: "summary", alias: "a", instr: "i", maxOutput: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSpec(tt.key, tt.op, tt.alias, tt.instr, tt.data, tt.maxOutput); !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("error = %v, want ErrInvalidSpec", err)
			}
		})
	}
}

func TestNewSpecPayloadLimit(t *testing.T) {
	for _, c := range []struct{ instr, data int }{{MaxPayloadBytes + 1, 0}, {MaxPayloadBytes, 1}, {1, MaxPayloadBytes}, {1, 2 * MaxPayloadBytes}} {
		_, err := NewSpec("k", "summary", "a", strings.Repeat("i", c.instr), strings.Repeat("d", c.data), 0)
		if !errors.Is(err, ErrPayloadTooLarge) || !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("%d + %d bytes: error = %v, want ErrPayloadTooLarge", c.instr, c.data, err)
		}
	}
	if _, err := NewSpec("k", "summary", "a", strings.Repeat("i", MaxPayloadBytes-1), "d", 0); err != nil {
		t.Fatalf("exactly 1 MiB: %v", err)
	}
}

func TestSpecFingerprint(t *testing.T) {
	spec := func(key, op, alias, instr, data string, maxOutput int64) Spec {
		t.Helper()
		s, err := NewSpec(key, op, alias, instr, data, maxOutput)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	base := spec("k1", "summary", "default", "ab", "cd", 10)
	fp := base.Fingerprint()
	if len(fp) != 64 || strings.Trim(fp, "0123456789abcdef") != "" {
		t.Fatalf("fingerprint %q is not lower-case hex SHA-256", fp)
	}
	if spec("other-key", "summary", "default", "ab", "cd", 10).Fingerprint() != fp {
		t.Fatal("the key must not change the fingerprint")
	}
	for name, other := range map[string]Spec{
		"operation":       spec("k1", "homework_draft", "default", "ab", "cd", 10),
		"alias":           spec("k1", "summary", "fast", "ab", "cd", 10),
		"instructions":    spec("k1", "summary", "default", "ax", "cd", 10),
		"data":            spec("k1", "summary", "default", "ab", "cx", 10),
		"max output":      spec("k1", "summary", "default", "ab", "cd", 0),
		"boundary moved":  spec("k1", "summary", "default", "abc", "d", 10),
		"data into instr": spec("k1", "summary", "default", "abcd", "", 10),
	} {
		if other.Fingerprint() == fp {
			t.Errorf("%s: same fingerprint for a different spec", name)
		}
	}
}

func TestSpecPrintsNoText(t *testing.T) {
	s, err := NewSpec("secret-key", "summary", "default", "secret-instructions", "secret-data", 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprintf("%v", &s)} {
		if strings.Contains(out, "secret") {
			t.Errorf("printed spec leaks text: %s", out)
		}
	}
}
