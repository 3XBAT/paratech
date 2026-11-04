package e2ehooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSafeName(t *testing.T) {
	tests := []struct{ name, want string }{
		{"Расшифровка 04.11.2026.txt", "Расшифровка 04.11.2026.txt"},
		{"paratech-support.zip", "paratech-support.zip"},
		{"../../evil.txt", "evil.txt"},
		{`..\..\evil.txt`, "evil.txt"},
		{`C:\Users\x\evil.docx`, "evil.docx"},
		{"C:evil.txt", "C_evil.txt"},
		{"a.wav:stream", "a.wav_stream"},
		{"a.wav::$DATA", "a.wav__$DATA"},
		{`a<b>c|d?e*f"g.md`, "a_b_c_d_e_f_g.md"},
		{"line\nbreak\x00.txt", "line_break_.txt"},
		{"x.txt.", "x.txt"},
		{"x.txt . .", "x.txt"},
		{"  lead.txt", "lead.txt"},
		{"CON", "_CON"},
		{"nul.txt", "_nul.txt"},
		{"com1.docx", "_com1.docx"},
		{"", fallbackName},
		{".", fallbackName},
		{"..", fallbackName},
		{"...", fallbackName},
		{"   ", fallbackName},
		{"dir/", fallbackName},
		{"\xff\xfe.txt", "__.txt"},
	}
	for _, tt := range tests {
		if got := safeName(tt.name); got != tt.want {
			t.Errorf("safeName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
	long := safeName(strings.Repeat("я", 500) + ".txt")
	if utf8.RuneCountInString(long) != maxNameRunes {
		t.Errorf("a long name has %d runes, want %d", utf8.RuneCountInString(long), maxNameRunes)
	}
}

func TestSaveFile(t *testing.T) {
	ctx := context.Background()
	r := newRun(t)

	got, err := r.h.SaveFile(ctx, "transcript", "../Расшифровка.txt")
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(filepath.Join(r.dir, savedDir))
	if err != nil {
		t.Fatalf("saved/ was not created: %v", err)
	}
	if want := filepath.Join(real, "Расшифровка.txt"); got != want {
		t.Fatalf("SaveFile = %q, want %q", got, want)
	}

	// The writer replaces an existing file; the dialog gives the same path again.
	writeFile(t, got, "old")
	again, _ := r.h.SaveFile(ctx, "transcript", "Расшифровка.txt")
	if again != got {
		t.Fatalf("existing file: %q, want %q", again, got)
	}

	// A folder at the target name cancels.
	if err := os.Mkdir(filepath.Join(real, "dir.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.h.SaveFile(ctx, "knowledge", "dir.md"); got != "" {
		t.Fatalf("a folder at the target: %q", got)
	}
	if !strings.Contains(r.logs.String(), "level=ERROR") {
		t.Fatalf("no error line for the cancel:\n%s", r.logs.String())
	}

	// saved/ as a file cancels.
	r = newRun(t)
	writeFile(t, filepath.Join(r.dir, savedDir), "not a folder")
	if got, _ := r.h.SaveFile(ctx, "bundle", "b.zip"); got != "" {
		t.Fatalf("saved is a file: %q", got)
	}
}
