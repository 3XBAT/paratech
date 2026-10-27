package whispercli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestBuildArgs(t *testing.T) {
	base := []string{
		"-m", "/d/models/m.bin",
		"-f", "/d/recordings/-a b.wav",
		"-of", "/d/work/run-1/out",
		"-oj", "-pp", "-ng",
		"-t", "4",
		"-l", "ru",
	}
	tests := []struct {
		name string
		vad  string
		want []string
	}{
		{"without VAD", "", base},
		// Only silence is skipped: no beam or best-of flags, the decoder stays the CLI default (owner, 2026-10-27).
		{"with VAD", "/app/ggml-silero-v5.1.2.bin", append(slices.Clone(base), "--vad", "-vm", "/app/ggml-silero-v5.1.2.bin")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildArgs("/d/models/m.bin", "/d/recordings/-a b.wav", "/d/work/run-1/out", 4, "ru", tt.vad)
			if !slices.Equal(got, tt.want) {
				t.Errorf("args = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckArg(t *testing.T) {
	abs := filepath.Join(string(filepath.Separator)+"data", "a b", "c.wav")
	if filepath.VolumeName(os.TempDir()) != "" {
		abs = filepath.VolumeName(os.TempDir()) + abs
	}

	tests := []struct {
		name string
		arg  string
		ok   bool
	}{
		{"absolute path with spaces", abs, true},
		{"quote and dollar", "/d/it's $HOME", true},
		{"empty", "", false},
		{"flag", "-m", false},
		{"double dash", "--", false},
		{"stdin marker", "-", false},
		{"response file", "@args", false},
		{"newline", "/d/a\nb", false},
		{"carriage return", "/d/a\rb", false},
		{"NUL", "/d/a\x00b", false},
		{"escape", "/d/a\x1bb", false},
		{"C1 control", "/d/a\u0085b", false},
		{"invalid UTF-8", "/d/a\xffb", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkArg(tt.arg)
			if tt.ok && err != nil {
				t.Fatalf("checkArg: %v", err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestWithin(t *testing.T) {
	root := filepath.Join(os.TempDir(), "root")
	tests := []struct {
		p    string
		want bool
	}{
		{filepath.Join(root, "a"), true},
		{filepath.Join(root, "a", "b"), true},
		{root, false},
		{filepath.Dir(root), false},
		{root + "x", false},
		{filepath.Join(root, "..", "x"), false},
		{"relative", false},
	}
	for _, tt := range tests {
		if got := within(root, tt.p); got != tt.want {
			t.Errorf("within(%q) = %v, want %v", tt.p, got, tt.want)
		}
	}
}

func TestWantKind(t *testing.T) {
	ref, _ := shared.NewArtifactRef(shared.ArtifactModels, "m.bin")
	if err := wantKind("model", ref, shared.ArtifactModels); err != nil {
		t.Errorf("same kind: %v", err)
	}
	if err := wantKind("model", ref, shared.ArtifactRecordings); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("other kind: %v", err)
	}
	if err := wantKind("model", shared.ArtifactRef{}, shared.ArtifactModels); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("zero ref: %v", err)
	}
}

func TestLanguagesAreLowerCaseCodes(t *testing.T) {
	if len(languages) != 101 {
		t.Errorf("%d languages, want 100 codes of whisper.cpp v1.9.5 plus auto", len(languages))
	}
	for code := range languages {
		for _, r := range code {
			if r < 'a' || r > 'z' {
				t.Errorf("code %q is not lower-case ASCII", code)
			}
		}
	}
}
