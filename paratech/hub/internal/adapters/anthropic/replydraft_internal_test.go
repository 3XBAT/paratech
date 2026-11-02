package anthropic

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// feedAll feeds chunks in order and returns the joined output and every output chunk.
func feedAll(chunks ...string) (string, []string) {
	var r replyText
	var all strings.Builder
	var outs []string
	for _, c := range chunks {
		out := r.Feed(c)
		if !utf8.ValidString(out) {
			panic("invalid UTF-8 out of Feed")
		}
		outs = append(outs, out)
		all.WriteString(out)
	}
	return all.String(), outs
}

// u is the JSON escape of the UTF-16 code unit hex, built so that no tool rewrites it in this file.
func u(hex string) string { return string(rune('\\')) + "u" + hex }

// splitEvery cuts s into byte chunks of n, so escapes and multibyte characters are split in every place.
func splitEvery(s string, n int) []string {
	var out []string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	return append(out, s)
}

// TestReplyTextDecodes: the text argument is decoded like encoding/json would, whatever the chunk boundaries.
func TestReplyTextDecodes(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"plain":             {`{"text":"Привет, мир"}`, "Привет, мир"},
		"escapes":           {`{"text":"a\"b\\c\/d\ne\tf"}`, "a\"b\\c/d\ne\tf"},
		"unicode escape":    {`{"text":"` + u("041f") + u("0440") + u("0438") + `"}`, "При"},
		"surrogate pair":    {`{"text":"x` + u("d83d") + u("de00") + `y"}`, "x😀y"},
		"upper hex":         {`{"text":"` + u("041F") + `"}`, "П"},
		"lone high":         {`{"text":"x\ud83dy"}`, "x�y"},
		"lone high at end":  {`{"text":"x\ud83d"}`, "x�"},
		"lone low":          {`{"text":"x\ude00y"}`, "x�y"},
		"high then escape":  {`{"text":"\ud83d\n"}`, "�\n"},
		"emoji raw":         {`{"text":"😀 ok"}`, "😀 ok"},
		"spaces":            {`{ "text" : "a" }`, "a"},
		"after other key":   {`{"card_ids":["x","y"],"text":"t"}`, "t"},
		"before other key":  {`{"text":"t","card_ids":["x"]}`, "t"},
		"controls dropped":  {`{"text":"a\u0007b\rc"}`, "abc"},
		"html stays text":   {`{"text":"<b>&amp;</b>"}`, "<b>&amp;</b>"},
		"empty":             {`{"text":""}`, ""},
		"no text":           {`{"other":"secret"}`, ""},
		"nested text":       {`{"a":{"text":"secret"},"b":[{"text":"secret"}]}`, ""},
		"text as value":     {`{"other":"text","x":"secret"}`, ""},
		"key prefix":        {`{"texts":"secret","xtext":"secret"}`, ""},
		"escaped key":       {`{"t` + u("0065") + `xt":"secret"}`, ""},
		"number text":       {`{"text":5,"x":"secret"}`, ""},
		"first text only":   {`{"text":"a","text":"secret"}`, "a"},
		"not an object":     {`["text","secret"]`, ""},
		"string top level":  {`"text"`, ""},
		"bad escape stops":  {`{"text":"a\x41bc"}`, "a"},
		"bad hex stops":     {`{"text":"a\u00zzb"}`, "a"},
		"cut mid text":      {`{"text":"abc`, "abc"},
		"after close":       {`{"text":"a"}{"text":"secret"}`, "a"},
		"long other key":    {`{"` + strings.Repeat("k", 100) + `":"secret","text":"ok"}`, "ok"},
		"escaped quote key": {`{"a\"text":"secret","text":"ok"}`, "ok"},
	}
	for name, tc := range cases {
		for _, n := range []int{len(tc.in) + 1, 1, 2, 3, 5, 7} {
			got, _ := feedAll(splitEvery(tc.in, n)...)
			if got != tc.want {
				t.Errorf("%s (chunks of %d): got %q, want %q", name, n, got, tc.want)
			}
		}
	}
}

// TestReplyTextKeepsRunesWhole: a multibyte character split between chunks comes out whole in the later chunk, never
// as two replacement characters.
func TestReplyTextKeepsRunesWhole(t *testing.T) {
	in := `{"text":"я😀"}`
	_, outs := feedAll(splitEvery(in, 1)...)
	for _, o := range outs {
		if strings.ContainsRune(o, utf8.RuneError) {
			t.Fatalf("chunk %q has U+FFFD (outputs %q)", o, outs)
		}
	}
	got, _ := feedAll(in[:10], in[10:12], in[12:])
	if got != "я😀" {
		t.Fatalf("got %q", got)
	}
	// Bytes that never become a character are replaced once the text ends.
	got, _ = feedAll(`{"text":"a`, "\xd0", `"}`)
	if got != "a�" {
		t.Fatalf("got %q", got)
	}
}

// TestReplyTextStreamsAsItArrives: each chunk returns its own part of the text at once.
func TestReplyTextStreamsAsItArrives(t *testing.T) {
	_, outs := feedAll(`{"te`, `xt":"Пер`, `вая часть`, `, вторая\n`, `"}`)
	want := []string{"", "Пер", "вая часть", ", вторая\n", ""}
	if strings.Join(outs, "|") != strings.Join(want, "|") {
		t.Fatalf("outs %q, want %q", outs, want)
	}
}

// TestReplyTextIsBounded: the reader keeps a few bytes of state whatever it reads, and malformed input never panics.
func TestReplyTextIsBounded(t *testing.T) {
	var r replyText
	_ = r.Feed(`{"` + strings.Repeat("k", 1<<16) + `":"` + strings.Repeat("v", 1<<16))
	if cap(r.key) > 64 || len(r.tail) > utf8.UTFMax || len(r.esc) > 5 {
		t.Fatalf("state grew: key %d, tail %d, esc %d", cap(r.key), len(r.tail), len(r.esc))
	}
	big := strings.Repeat("ж", 10000)
	got, _ := feedAll(splitEvery(`{"text":"`+big+`"}`, 4096)...)
	if got != big {
		t.Fatalf("got %d runes", utf8.RuneCountInString(got))
	}
	for _, junk := range []string{"", "}", "]]]]", `{"text":`, `{"text":"\`, `{"text":"\u`, `{"text":"\ud83d\u`, "\x00\xff{\"\\", `{{{{"text":"x"`} {
		var j replyText
		for _, c := range splitEvery(junk, 1) {
			if out := j.Feed(c); !utf8.ValidString(out) {
				t.Fatalf("%q: invalid output", junk)
			}
		}
	}
}

// FuzzReplyText: no input panics or yields invalid UTF-8.
func FuzzReplyText(f *testing.F) {
	for _, s := range []string{`{"text":"a` + u("d83d") + u("de00") + `"}`, `{"text":"\u00`, `[{"text":1}]`} {
		f.Add(s, 3)
	}
	f.Fuzz(func(t *testing.T, s string, n int) {
		if n <= 0 || n > 64 {
			n = 1
		}
		var r replyText
		for _, c := range splitEvery(s, n) {
			if out := r.Feed(c); !utf8.ValidString(out) {
				t.Fatalf("invalid output %q", out)
			}
		}
	})
}
