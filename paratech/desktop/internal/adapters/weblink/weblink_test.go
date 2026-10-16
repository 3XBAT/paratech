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

// googleAuthPage has the shape of the URL x/oauth2 builds for the Google sign-in (url.Values.Encode, sorted keys).
const googleAuthPage = "https://accounts.google.com/o/oauth2/v2/auth?access_type=offline" +
	"&client_id=123-fake.apps.googleusercontent.com&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" +
	"&code_challenge_method=S256&prompt=consent&redirect_uri=http%3A%2F%2F127.0.0.1%3A53682&response_type=code" +
	"&scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fcalendar.app.created&state=Zm9v_YmFy-YmF6"

func TestCheckAuthLink(t *testing.T) {
	allowed := []string{
		googleAuthPage,
		"https://example.com/auth?a=1",
		"https://example.com/auth?a=%7e&b=%7E&c=-._~",
		"https://example.com/auth?" + strings.Repeat("a", maxAuthLinkLen-len("https://example.com/auth?")),
	}
	for _, s := range allowed {
		if err := checkAuthLink(s); err != nil {
			t.Errorf("checkAuthLink(%q) = %v, want nil", s, err)
		}
	}

	refused := []string{
		"", "https://example.com/auth", "https://example.com/auth?", "http://example.com/auth?a=1",
		"HTTPS://example.com/auth?a=1", "https://Example.com/auth?a=1", "https://example.com:8443/auth?a=1",
		"https://user@example.com/auth?a=1", "https://localhost/auth?a=1", "https://[::1]/auth?a=1",
		"https://example.com/?a=1", "https://example.com/a/../auth?a=1", "https://example.com//auth?a=1",
		"https://example.com/auth?a=1#frag", "https://example.com/auth?a=1?b", "https://example.com/auth?a=1+2",
		"https://example.com/auth?a=1,2", "https://example.com/auth?a=1;2", `https://example.com/auth?a="1"`,
		"https://example.com/auth?a='1'", "https://example.com/auth?a=1 2", "https://example.com/auth?a=1\t",
		"https://example.com/auth?a=^1", "https://example.com/auth?a=!1", "https://example.com/auth?a=<1>",
		"https://example.com/auth?a=1|2", "https://example.com/auth?a=`1`", "https://example.com/auth?a=%USERPROFILE%",
		"https://example.com/auth?a=%", "https://example.com/auth?a=%2", "https://example.com/auth?a=%zz",
		"https://example.com/auth?a=1/2", "https://example.com/auth?a=\\1", "https://example.com/auth?a=1\x00",
		"https://example.com/auth?a=é", "https://example.com/auth?a=(1)", "https://example.com/auth?a=$1",
		"https://example.com/auth?a=*", "https://example.com/auth?a=@", "https://example.com/auth?a=1\r\n",
		"https://example.com/auth?" + strings.Repeat("a", maxAuthLinkLen),
	}
	for _, s := range refused {
		if err := checkAuthLink(s); !errors.Is(err, ErrRefused) {
			t.Errorf("checkAuthLink(%q) = %v, want ErrRefused", s, err)
		}
	}
}

// A refused authorization page never reaches the launcher, on every system.
func TestOpenAuthPageRefusesBeforeTheStart(t *testing.T) {
	for _, s := range []string{"", "https://example.com/auth?a=1,2", "https://example.com/auth"} {
		if err := New().OpenAuthPage(context.Background(), s); !errors.Is(err, ErrRefused) {
			t.Fatalf("OpenAuthPage(%q) = %v, want ErrRefused", s, err)
		}
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
