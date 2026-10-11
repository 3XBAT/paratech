package shared

import (
	"errors"
	"strings"
	"testing"
)

func TestIDValidate(t *testing.T) {
	validators := map[string]func(string) error{
		"RecordingID":  func(s string) error { return RecordingID(s).Validate() },
		"JobID":        func(s string) error { return JobID(s).Validate() },
		"TranscriptID": func(s string) error { return TranscriptID(s).Validate() },
		"SttModelID":   func(s string) error { return SttModelID(s).Validate() },
		"LessonID":     func(s string) error { return LessonID(s).Validate() },
		"AssignmentID": func(s string) error { return AssignmentID(s).Validate() },
		"DeliveryID":   func(s string) error { return DeliveryID(s).Validate() },
	}
	cases := []struct {
		name    string
		id      string
		invalid bool
	}{
		{"uuid", "0b9d6f3e-4c1a-4d6e-9b53-6f1c2a7e8d10", false},
		{"deterministic catalog id", "ggml-base-q5_1@1", false},
		{"empty", "", true},
		{"space", "a b", true},
		{"newline", "a\nb", true},
		{"nul", "a\x00b", true},
		{"too long", strings.Repeat("x", maxIDLen+1), true},
		{"max length", strings.Repeat("x", maxIDLen), false},
	}
	for typ, validate := range validators {
		for _, tc := range cases {
			t.Run(typ+"/"+tc.name, func(t *testing.T) {
				err := validate(tc.id)
				if tc.invalid != (err != nil) {
					t.Fatalf("Validate(%q) error = %v, want invalid=%v", tc.id, err, tc.invalid)
				}
				if err != nil && !errors.Is(err, ErrInvalidID) {
					t.Fatalf("error %v does not wrap ErrInvalidID", err)
				}
			})
		}
	}
}

func TestIDString(t *testing.T) {
	if got := RecordingID("r1").String(); got != "r1" {
		t.Fatalf("String() = %q", got)
	}
}

func TestNewArtifactRef(t *testing.T) {
	cases := []struct {
		name    string
		kind    ArtifactKind
		path    string
		invalid bool
	}{
		{"file", ArtifactRecordings, "r1/chunk-0001.wav", false},
		{"directory", ArtifactRecordings, "r1", false},
		{"model", ArtifactModels, "ggml-base.bin", false},
		{"unknown kind", ArtifactKind("secrets"), "x", true},
		{"empty kind", ArtifactKind(""), "x", true},
		{"empty path", ArtifactExports, "", true},
		{"parent segment", ArtifactExports, "../etc/passwd", true},
		{"embedded parent", ArtifactExports, "a/../../b", true},
		{"dot segment", ArtifactExports, "a/./b", true},
		{"absolute", ArtifactExports, "/etc/passwd", true},
		{"trailing slash", ArtifactExports, "a/", true},
		{"double slash", ArtifactExports, "a//b", true},
		{"backslash", ArtifactExports, `a\b`, true},
		{"drive letter", ArtifactExports, "C:/Windows", true},
		{"control char", ArtifactExports, "a\x00b", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := NewArtifactRef(tc.kind, tc.path)
			if tc.invalid {
				if !errors.Is(err, ErrInvalidArtifactRef) {
					t.Fatalf("error = %v, want ErrInvalidArtifactRef", err)
				}
				if !ref.IsZero() {
					t.Fatalf("invalid input produced %v", ref)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ref.Kind() != tc.kind || ref.Path() != tc.path {
				t.Fatalf("got %v/%v", ref.Kind(), ref.Path())
			}
			back, err := ParseArtifactRef(ref.String())
			if err != nil || back != ref {
				t.Fatalf("ParseArtifactRef(%q) = %v, %v; want %v", ref.String(), back, err, ref)
			}
		})
	}
}

func TestParseArtifactRefRejects(t *testing.T) {
	for _, s := range []string{"", "recordings", "recordings/", "unknown/x", "/recordings/x", "recordings/../x"} {
		if _, err := ParseArtifactRef(s); !errors.Is(err, ErrInvalidArtifactRef) {
			t.Errorf("ParseArtifactRef(%q) error = %v, want ErrInvalidArtifactRef", s, err)
		}
	}
}

func TestArtifactRefChild(t *testing.T) {
	dir, err := NewArtifactRef(ArtifactRecordings, "r1")
	if err != nil {
		t.Fatal(err)
	}
	child, err := dir.Child("chunk-0001.wav")
	if err != nil {
		t.Fatal(err)
	}
	if got := child.String(); got != "recordings/r1/chunk-0001.wav" {
		t.Fatalf("Child = %q", got)
	}
	for _, name := range []string{"", "..", "a/b", `a\b`} {
		if _, err := dir.Child(name); !errors.Is(err, ErrInvalidArtifactRef) {
			t.Errorf("Child(%q) error = %v, want ErrInvalidArtifactRef", name, err)
		}
	}
	if _, err := (ArtifactRef{}).Child("x"); !errors.Is(err, ErrInvalidArtifactRef) {
		t.Errorf("Child on zero ref error = %v", err)
	}
}
