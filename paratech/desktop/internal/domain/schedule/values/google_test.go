package values

import (
	"errors"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestParseGoogleClientID(t *testing.T) {
	const ok = "123-fake.apps.googleusercontent.com"
	tests := []struct {
		in, want string // want "" means refused
	}{
		{in: ok, want: ok},
		{in: " \t" + ok + "\r\n", want: ok},
		{in: "123456789012-abc0def1ghi2.apps.googleusercontent.com", want: "123456789012-abc0def1ghi2.apps.googleusercontent.com"},
		{in: ""},
		{in: ".apps.googleusercontent.com"},
		{in: "123-fake"},
		{in: "123-fake.apps.googleusercontent.com.evil.example"},
		{in: "123-Fake.apps.googleusercontent.com"},
		{in: "123_fake.apps.googleusercontent.com"},
		{in: "123.fake.apps.googleusercontent.com"},
		{in: "-123.apps.googleusercontent.com"},
		{in: "123-.apps.googleusercontent.com"},
		{in: "123 fake.apps.googleusercontent.com"},
		{in: "123-fakeé.apps.googleusercontent.com"},
		{in: "123&x=1.apps.googleusercontent.com"},
		{in: strings.Repeat("1", MaxGoogleClientIDLen) + ".apps.googleusercontent.com"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseGoogleClientID(tt.in)
			if tt.want == "" {
				if !errors.Is(err, ErrInvalidGoogleClientID) || got != "" {
					t.Fatalf("ParseGoogleClientID(%q) = %q, %v; want ErrInvalidGoogleClientID", tt.in, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("ParseGoogleClientID(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}

	longest := strings.Repeat("1", MaxGoogleClientIDLen-len(googleClientIDSuffix)) + googleClientIDSuffix
	if got, err := ParseGoogleClientID(longest); err != nil || got != longest {
		t.Fatalf("the longest id was refused: %v", err)
	}
}

func TestParseGoogleClientSecret(t *testing.T) {
	const ok = "GOCSPX-fake_Secret-123"
	tests := []struct {
		in, want string
	}{
		{in: ok, want: ok},
		{in: " " + ok + "\n", want: ok},
		{in: "!~", want: "!~"},
		{in: strings.Repeat("s", MaxGoogleClientSecretLen), want: strings.Repeat("s", MaxGoogleClientSecretLen)},
		{in: ""},
		{in: " \r\n"},
		{in: "fake secret"},
		{in: "fake\x00secret"},
		{in: "fake\x7fsecret"},
		{in: "fakeésecret"},
		{in: strings.Repeat("s", MaxGoogleClientSecretLen+1)},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseGoogleClientSecret(tt.in)
			if tt.want == "" {
				if !errors.Is(err, ErrInvalidGoogleClientSecret) || !errors.Is(err, shared.ErrInvalidSecret) || got != "" {
					t.Fatalf("ParseGoogleClientSecret = %q, %v; want ErrInvalidGoogleClientSecret", got, err)
				}
				if tt.in != "" && strings.Contains(err.Error(), tt.in) {
					t.Fatal("the error quotes the value")
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("ParseGoogleClientSecret = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestCheckGrantedScopes(t *testing.T) {
	tests := []struct {
		granted string
		want    error
	}{
		{granted: GoogleCalendarScope},
		{granted: " " + GoogleCalendarScope + " "},
		{granted: GoogleCalendarScope + " " + GoogleCalendarScope},
		{granted: "", want: ErrCalendarScopeNotGranted},
		{granted: "   ", want: ErrCalendarScopeNotGranted},
		{granted: "https://www.googleapis.com/auth/calendar", want: ErrScopeTooBroad},
		{granted: GoogleCalendarScope + " https://www.googleapis.com/auth/calendar", want: ErrScopeTooBroad},
		{granted: "openid " + GoogleCalendarScope, want: ErrScopeTooBroad},
		{granted: "https://www.googleapis.com/auth/calendar.app.created.x", want: ErrScopeTooBroad},
		{granted: "HTTPS://www.googleapis.com/auth/calendar.app.created", want: ErrScopeTooBroad},
	}
	for _, tt := range tests {
		t.Run(tt.granted, func(t *testing.T) {
			if err := CheckGrantedScopes(tt.granted); !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) {
				t.Fatalf("CheckGrantedScopes(%q) = %v, want %v", tt.granted, err, tt.want)
			}
		})
	}
}
