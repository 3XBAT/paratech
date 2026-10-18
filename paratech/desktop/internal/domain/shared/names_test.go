package shared

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSafeFileName(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "Математика 2026-10-18", "Математика 2026-10-18"},
		{"forbidden characters", `a\b/c:d*e?f"g<h>i|j`, "a_b_c_d_e_f_g_h_i_j"},
		{"control characters", "a\x00b\nc\u0085d", "a_b c d"}, // line breaks are spaces
		{"delete character", "ab", "a_b"},
		{"spaces collapse and trim", "  a \t  b  ", "a b"},
		{"trailing dots", "lesson...", "lesson"},
		{"reserved name", "CON", "_CON"},
		{"reserved name with extension", "nul.txt", "_nul.txt"},
		{"reserved name with space", "Aux .md", "_Aux .md"},
		{"not reserved", "console", "console"},
		{"only dots", "...", ""},
		{"empty", "", ""},
		{"dot dot", "..", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SafeFileName(tc.in)
			if got != tc.want {
				t.Fatalf("SafeFileName(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if got != "" && nameProblem(got) != "" {
				t.Fatalf("SafeFileName(%q) = %q is not a valid name: %s", tc.in, got, nameProblem(got))
			}
		})
	}
}

func TestSafeFileNameLength(t *testing.T) {
	got := SafeFileName(strings.Repeat("я", 500))
	if n := utf8.RuneCountInString(got); n != maxFileNameRunes {
		t.Fatalf("length %d runes, want %d", n, maxFileNameRunes)
	}
	// A cut that ends in a space or dot is trimmed again.
	got = SafeFileName(strings.Repeat("a", maxFileNameRunes-1) + ". tail")
	if strings.HasSuffix(got, ".") || strings.HasSuffix(got, " ") || nameProblem(got) != "" {
		t.Fatalf("cut name %q ends badly", got)
	}
}
