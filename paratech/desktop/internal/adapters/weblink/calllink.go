package weblink

import (
	"context"
	"fmt"

	schedvalues "paratech/desktop/internal/domain/schedule/values"
)

// OpenCallLink opens the online call of a lesson (ADR-0032, «Opening and copying»). The canonical form is re-parsed
// right before the start and, like OpenAuthPage, always goes through a temporary .url file: the link never reaches a
// command line. It returns like OpenLink.
func (o *Opener) OpenCallLink(ctx context.Context, l schedvalues.CallLink) error {
	link, err := checkCallLink(l.String())
	if err != nil {
		return err
	}
	return startShortcut(ctx, link)
}

// checkCallLink accepts s only when ParseCallLink accepts it and gives back exactly s, so nothing is trimmed,
// lowercased or otherwise rewritten between the check and the .url file. The byte loop keeps the .url defence local:
// a CR, LF or any other control byte would start a new line of the file.
func checkCallLink(s string) (string, error) {
	parsed, err := schedvalues.ParseCallLink(s)
	if err != nil {
		return "", fmt.Errorf("%w: not a call link", ErrRefused)
	}
	if parsed.String() != s {
		return "", fmt.Errorf("%w: the call link is not in canonical form", ErrRefused)
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x21 || c > 0x7e || c == '"' {
			return "", fmt.Errorf("%w: the call link has a character that is not allowed", ErrRefused)
		}
	}
	return s, nil
}
