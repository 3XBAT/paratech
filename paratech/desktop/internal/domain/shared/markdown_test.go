package shared

import "testing"

// The detailed cases live with the transcript export (transcript.TestEscapeMarkdown), which used this escaper first.
func TestEscapeMarkdown(t *testing.T) {
	tests := []struct{ in, want string }{
		{"обычный текст", "обычный текст"},
		{"<b>x</b> [a](b)", `\<b\>x\</b\> \[a\](b)`},
		{"- пункт", `\- пункт`},
		{"1. первое", `1\. первое`},
		{"&lt;", "&amp;lt;"},
		{"https://e.com", `https:\//e.com`},
		{"", ""},
	}
	for _, tc := range tests {
		if got := EscapeMarkdown(tc.in); got != tc.want {
			t.Errorf("EscapeMarkdown(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
