package values

import (
	"errors"
	"strings"
	"testing"
)

func TestLabels(t *testing.T) {
	cases := []struct {
		name  string
		value string
		ok    bool
	}{
		{"simple", "base", true},
		{"dashes and dots", "large-v3-turbo.en", true},
		{"underscore", "q5_1", true},
		{"upper case", "Base", true},
		{"digits only", "1", true},
		{"empty", "", false},
		{"leading dot", ".base", false},
		{"trailing dot", "base.", false},
		{"leading dash", "-base", false},
		{"trailing underscore", "base_", false},
		{"slash", "a/b", false},
		{"backslash", `a\b`, false},
		{"space", "a b", false},
		{"colon", "a:b", false},
		{"non-ascii", "бейс", false},
		{"control", "a\x00b", false},
	}
	validators := map[string]struct {
		check  func(string) error
		err    error
		maxLen int
	}{
		"ModelName":    {func(s string) error { return ModelName(s).Validate() }, ErrInvalidModelName, MaxModelNameLen},
		"ModelVersion": {func(s string) error { return ModelVersion(s).Validate() }, ErrInvalidModelVersion, MaxModelVersionLen},
		"CatalogID":    {func(s string) error { return CatalogID(s).Validate() }, ErrInvalidCatalogID, MaxCatalogIDLen},
	}

	for kind, v := range validators {
		for _, tc := range cases {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				err := v.check(tc.value)
				if tc.ok != (err == nil) {
					t.Fatalf("Validate(%q) = %v, want ok=%v", tc.value, err, tc.ok)
				}
				if err != nil && !errors.Is(err, v.err) {
					t.Fatalf("error %v does not wrap %v", err, v.err)
				}
			})
		}
		t.Run(kind+"/length", func(t *testing.T) {
			if err := v.check(strings.Repeat("a", v.maxLen)); err != nil {
				t.Fatalf("max length: %v", err)
			}
			if err := v.check(strings.Repeat("a", v.maxLen+1)); !errors.Is(err, v.err) {
				t.Fatalf("over max length: %v", err)
			}
		})
	}
}

func TestQuantization(t *testing.T) {
	cases := []struct {
		value string
		ok    bool
	}{
		{"", true}, {"q5_1", true}, {"f16", true}, {"Q5_1", false}, {"q5-1", false}, {"q5.1", false},
		{strings.Repeat("q", MaxQuantizationLen), true}, {strings.Repeat("q", MaxQuantizationLen+1), false},
	}
	for _, tc := range cases {
		err := Quantization(tc.value).Validate()
		if tc.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidQuantization)) {
			t.Errorf("Quantization(%q).Validate() = %v, want ok=%v", tc.value, err, tc.ok)
		}
	}
}

func TestCapabilities(t *testing.T) {
	many := make([]Capability, MaxCapabilities+1)
	for i := range many {
		many[i] = Capability("c" + strings.Repeat("x", i))
	}
	cases := []struct {
		name string
		caps []Capability
		ok   bool
	}{
		{"none", nil, true},
		{"languages", []Capability{"multilingual", "en", "ru"}, true},
		{"dash and underscore", []Capability{"word-timestamps", "a_b"}, true},
		{"empty label", []Capability{""}, false},
		{"upper case", []Capability{"EN"}, false},
		{"leading dash", []Capability{"-en"}, false},
		{"space", []Capability{"e n"}, false},
		{"too long", []Capability{Capability(strings.Repeat("a", MaxCapabilityLen+1))}, false},
		{"repeated", []Capability{"en", "en"}, false},
		{"too many", many, false},
		{"max count", many[:MaxCapabilities], true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCapabilities(tc.caps)
			if tc.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidCapability)) {
				t.Fatalf("ValidateCapabilities(%v) = %v, want ok=%v", tc.caps, err, tc.ok)
			}
		})
	}
}

func TestSHA256(t *testing.T) {
	lower := strings.Repeat("0123456789abcdef", 4)
	cases := []struct {
		name  string
		input string
		want  SHA256
		ok    bool
	}{
		{"lowercase", lower, SHA256(lower), true},
		{"uppercase is canonicalised", strings.ToUpper(lower), SHA256(lower), true},
		{"too short", lower[:63], "", false},
		{"too long", lower + "0", "", false},
		{"empty", "", "", false},
		{"not hex", strings.Repeat("g", 64), "", false},
		{"space", " " + lower[1:], "", false},
		{"multibyte of the right byte length", strings.Repeat("é", 32), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSHA256(tc.input)
			if tc.ok != (err == nil) || got != tc.want {
				t.Fatalf("ParseSHA256(%q) = %q, %v; want %q ok=%v", tc.input, got, err, tc.want, tc.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidSHA256) {
				t.Fatalf("error %v does not wrap ErrInvalidSHA256", err)
			}
		})
	}
	if err := SHA256(strings.ToUpper(lower)).Validate(); !errors.Is(err, ErrInvalidSHA256) {
		t.Fatalf("Validate of a non-canonical digest = %v, want ErrInvalidSHA256", err)
	}
}

func TestValidateSize(t *testing.T) {
	for size, ok := range map[int64]bool{1: true, MaxModelSizeBytes: true, 0: false, -1: false, MaxModelSizeBytes + 1: false} {
		if err := ValidateSize(size); ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidSize)) {
			t.Errorf("ValidateSize(%d) = %v, want ok=%v", size, err, ok)
		}
	}
}

func TestSourceURL(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
		host  string
	}{
		{"plain", "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.bin",
			"https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.bin", "huggingface.co"},
		{"canonical scheme and host", "HTTPS://Example.COM/Models/a.bin", "https://example.com/Models/a.bin", "example.com"},
		{"port", "https://example.com:8443/a.bin", "https://example.com:8443/a.bin", "example.com:8443"},
		{"http", "http://example.com/a.bin", "", ""},
		{"file", "file:///etc/passwd", "", ""},
		{"ftp", "ftp://example.com/a.bin", "", ""},
		{"no scheme", "example.com/a.bin", "", ""},
		{"scheme relative", "//example.com/a.bin", "", ""},
		{"opaque", "https:example.com/a.bin", "", ""},
		{"user info", "https://user:secret@example.com/a.bin", "", ""},
		{"user only", "https://token@example.com/a.bin", "", ""},
		{"query", "https://example.com/a.bin?token=secret", "", ""},
		{"empty query", "https://example.com/a.bin?", "", ""},
		{"fragment", "https://example.com/a.bin#x", "", ""},
		{"empty fragment", "https://example.com/a.bin#", "", ""},
		{"no path", "https://example.com", "", ""},
		{"root path", "https://example.com/", "", ""},
		{"no host", "https:///a.bin", "", ""},
		{"empty port host", "https://:443/a.bin", "", ""},
		{"bad port", "https://example.com:x/a.bin", "", ""},
		{"space", "https://example.com/a b.bin", "", ""},
		{"backslash", `https://example.com\@evil.com/a.bin`, "", ""},
		{"control", "https://example.com/a\x00.bin", "", ""},
		{"non-ascii host", "https://ехample.com/a.bin", "", ""},
		{"empty", "", "", ""},
		{"too long", "https://example.com/" + strings.Repeat("a", MaxSourceURLLen), "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSourceURL(tc.input)
			if tc.want == "" {
				if !errors.Is(err, ErrInvalidSourceURL) || !got.IsZero() {
					t.Fatalf("ParseSourceURL(%q) = %q, %v; want ErrInvalidSourceURL", tc.input, got, err)
				}
				return
			}
			if err != nil || got.String() != tc.want || got.Host() != tc.host {
				t.Fatalf("ParseSourceURL(%q) = %q (host %q), %v; want %q (host %q)", tc.input, got, got.Host(), err, tc.want, tc.host)
			}
		})
	}
	if (SourceURL{}).Host() != "" {
		t.Fatal("the zero URL has a host")
	}
}

func TestMachine(t *testing.T) {
	m, err := NewMachine(4, 8192)
	if err != nil || !m.Known() || m.CPUCores() != 4 || m.RAMMB() != 8192 {
		t.Fatalf("NewMachine(4, 8192) = %+v, %v", m, err)
	}
	for _, bad := range [][2]int64{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := NewMachine(int(bad[0]), bad[1]); !errors.Is(err, ErrInvalidMachine) {
			t.Errorf("NewMachine(%d, %d) = %v, want ErrInvalidMachine", bad[0], bad[1], err)
		}
	}
	if (Machine{}).Known() {
		t.Fatal("the zero machine is known")
	}
}
