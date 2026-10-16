package values

import (
	"errors"
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/shared"
)

// GoogleCalendarScope is the only OAuth scope the app requests and accepts: access to the calendars the app created
// and nothing else (PRD section 11.3, AC-17).
const GoogleCalendarScope = "https://www.googleapis.com/auth/calendar.app.created"

// Bounds of the client values; real ones are far shorter (about 75 and 35 characters).
const (
	MaxGoogleClientIDLen     = 255
	MaxGoogleClientSecretLen = 255
)

// googleClientIDSuffix ends the id of every Google OAuth client.
const googleClientIDSuffix = ".apps.googleusercontent.com"

var (
	// ErrInvalidGoogleClientID means the value is not the id of a Google OAuth client. It never quotes the value.
	ErrInvalidGoogleClientID = errors.New("google client id must look like <number>-<letters and digits>" +
		googleClientIDSuffix)

	// ErrInvalidGoogleClientSecret means the value cannot be a client secret. It wraps shared.ErrInvalidSecret and
	// never quotes the value.
	ErrInvalidGoogleClientSecret = fmt.Errorf("%w: google client secret must be 1-%d printable ASCII characters without spaces",
		shared.ErrInvalidSecret, MaxGoogleClientSecretLen)

	// ErrCalendarScopeNotGranted means Google did not grant GoogleCalendarScope (the user unticked it).
	ErrCalendarScopeNotGranted = errors.New("google did not grant access to the app calendars")

	// ErrScopeTooBroad means Google granted a scope besides GoogleCalendarScope; the app must not hold it.
	ErrScopeTooBroad = errors.New("google granted more access than the app calendars")
)

// ParseGoogleClientID is the client id as the user pasted it: surrounding blanks are dropped, the rest must be ASCII
// [a-z0-9-] (not starting or ending with '-') followed by ".apps.googleusercontent.com", at most
// MaxGoogleClientIDLen bytes in total.
func ParseGoogleClientID(raw string) (string, error) {
	id := trimPaste(raw)
	head, ok := strings.CutSuffix(id, googleClientIDSuffix)
	if !ok || len(id) > MaxGoogleClientIDLen || head == "" || head[0] == '-' || head[len(head)-1] == '-' {
		return "", ErrInvalidGoogleClientID
	}
	for i := 0; i < len(head); i++ {
		c := head[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return "", ErrInvalidGoogleClientID
		}
	}
	return id, nil
}

// ParseGoogleClientSecret is the client secret as the user pasted it: surrounding blanks are dropped, the rest must
// be 1 to MaxGoogleClientSecretLen printable ASCII characters without spaces.
func ParseGoogleClientSecret(raw string) (string, error) {
	secret := trimPaste(raw)
	if secret == "" || len(secret) > MaxGoogleClientSecretLen {
		return "", ErrInvalidGoogleClientSecret
	}
	for i := 0; i < len(secret); i++ {
		if secret[i] < 0x21 || secret[i] > 0x7e {
			return "", ErrInvalidGoogleClientSecret
		}
	}
	return secret, nil
}

// CheckGrantedScopes checks the space-separated scope list of a Google token response: it must name
// GoogleCalendarScope and nothing else. An empty list grants nothing. The errors never quote the list.
func CheckGrantedScopes(granted string) error {
	scopes := strings.Fields(granted)
	found := false
	for _, s := range scopes {
		if s != GoogleCalendarScope {
			return ErrScopeTooBroad
		}
		found = true
	}
	if !found {
		return ErrCalendarScopeNotGranted
	}
	return nil
}

func trimPaste(s string) string { return strings.Trim(s, " \t\r\n") }
