package connect

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestDeltaSenderCutsOnlyBetweenRunes: a chunk that ends inside a 2-byte Cyrillic or a 4-byte emoji character sends
// its complete part; the rest waits for the next chunk, so every delta is valid UTF-8 and the text is unchanged.
func TestDeltaSenderCutsOnlyBetweenRunes(t *testing.T) {
	const ye, smile = "е", "😀"
	got := make(chan string)
	d := newDeltaSender(func(chunk string) error {
		got <- chunk
		return nil
	})
	steps := []struct{ push, want string }{
		{push: "обраща" + ye[:1], want: "обраща"},
		{push: ye[1:] + "тся " + smile[:2], want: ye + "тся "},
		{push: smile[2:3], want: ""}, // still incomplete: nothing is sent
		{push: smile[3:] + "!", want: smile + "!"},
	}
	var all strings.Builder
	for _, s := range steps {
		d.push(s.push)
		all.WriteString(s.push)
		if s.want == "" {
			continue
		}
		if c := <-got; c != s.want || !utf8.ValidString(c) {
			t.Fatalf("after %q: delta %q, want %q", s.push, c, s.want)
		}
	}

	// At the end an incomplete tail is sent as it is (the send makes it valid).
	d.push(ye[:1])
	finished := make(chan error, 1)
	go func() { finished <- d.finish(false) }()
	if c := <-got; c != ye[:1] {
		t.Fatalf("last delta %q", c)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if all.String() != "обращается "+smile+"!" {
		t.Fatalf("pushed %q", all.String())
	}
}

func TestCutIncompleteRune(t *testing.T) {
	for in, want := range map[string][2]string{
		"":                 {"", ""},
		"abc":              {"abc", ""},
		"ж":                {"ж", ""},
		"a\xd0":            {"a", "\xd0"},
		"a\xf0\x9f\x98":    {"a", "\xf0\x9f\x98"},
		"a\xf0\x9f":        {"a", "\xf0\x9f"},
		"a\xff":            {"a\xff", ""},
		"a\x80":            {"a\x80", ""},
		"a\xe0\x80":        {"a\xe0\x80", ""}, // can never become valid
		"\x80\x80\x80\x80": {"\x80\x80\x80\x80", ""},
		"😀":                {"😀", ""},
	} {
		if h, tl := cutIncompleteRune(in); h != want[0] || tl != want[1] {
			t.Errorf("cutIncompleteRune(%q) = %q, %q; want %q, %q", in, h, tl, want[0], want[1])
		}
	}
}
