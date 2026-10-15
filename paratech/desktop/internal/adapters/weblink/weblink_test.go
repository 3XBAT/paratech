package weblink

import (
	"context"
	"errors"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/transcription/values"
)

func TestLinkArg(t *testing.T) {
	tests := []struct {
		in   string
		want string // "" means refused
	}{
		{in: "https://huggingface.co/ggerganov/whisper.cpp", want: "https://huggingface.co/ggerganov/whisper.cpp"},
		{in: "https://HuggingFace.co/ggerganov/whisper.cpp/blob/main/ggml-base.en-q5_1.bin",
			want: "https://huggingface.co/ggerganov/whisper.cpp/blob/main/ggml-base.en-q5_1.bin"},
		{in: "https://example.com/a~b_c-d/", want: "https://example.com/a~b_c-d/"},
		{in: "https://example.com:8443/model"},
		{in: "https://example.com/a%20b"},
		{in: "https://example.com/a,b"},
		{in: "https://example.com/a;b"},
		{in: "https://example.com/%USERPROFILE%"},
		{in: "https://example.com/a^b"},
		{in: "https://example.com/a&b"},
		{in: "https://example.com/a=b"},
		{in: "https://example.com/a!b"},
		{in: "https://example.com/a'b"},
		{in: "https://example.com/a(b)"},
		{in: "https://example.com/a+b"},
		{in: "https://example.com/a@b"},
		{in: "https://example.com/a$b"},
		{in: "https://example.com/a*b"},
		{in: "https://example.com/../b"},
		{in: "https://example.com/a/./b"},
		{in: "https://example.com//b"},
		{in: "https://localhost/b"},
		{in: "https://1.2.3.4./b"},
		{in: "https://[::1]/b"},
		{in: "https://-x.com/b"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			u, err := values.ParseSourceURL(tt.in)
			if err != nil {
				if tt.want != "" {
					t.Fatalf("ParseSourceURL(%q): %v", tt.in, err)
				}
				return // the domain already refuses it
			}
			got, err := linkArg(u)
			if tt.want == "" {
				if !errors.Is(err, ErrRefused) {
					t.Fatalf("linkArg(%q) = %q, %v; want ErrRefused", tt.in, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("linkArg(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

// checkLink is the last check before the start; it refuses strings the domain value would never hold, too.
func TestCheckLinkRefuses(t *testing.T) {
	for _, s := range []string{
		"", "http://example.com/a", "-https://example.com/a", "/https://example.com/a", "https://example.com",
		"https://example.com/", `https://example.com/a"b`, "https://example.com/a b", "https://example.com/a\tb",
		"https://example.com/a\\b", "https://example.com/a?b", "https://example.com/a#b", "https://example.com/a\x00",
		"https://example.com/aé", "https://ex_ample.com/a", "https://user@example.com/a", "HTTPS://example.com/a",
		"https://example.com/" + strings.Repeat("a", maxLinkLen),
	} {
		if err := checkLink(s); !errors.Is(err, ErrRefused) {
			t.Errorf("checkLink(%q) = %v, want ErrRefused", s, err)
		}
	}
	if err := checkLink("https://example.com/a"); err != nil {
		t.Fatalf("a plain link was refused: %v", err)
	}
}

// A refused link never reaches the launcher, on every system.
func TestOpenLinkRefusesBeforeTheStart(t *testing.T) {
	if err := New().OpenLink(context.Background(), values.SourceURL{}); !errors.Is(err, ErrRefused) {
		t.Fatalf("OpenLink(zero) = %v, want ErrRefused", err)
	}
	u, err := values.ParseSourceURL("https://example.com/a,b")
	if err != nil {
		t.Fatal(err)
	}
	if err := New().OpenLink(context.Background(), u); !errors.Is(err, ErrRefused) {
		t.Fatalf("OpenLink(%s) = %v, want ErrRefused", u, err)
	}
}
