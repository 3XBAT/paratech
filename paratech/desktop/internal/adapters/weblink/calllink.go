package weblink

import (
	"context"
	"fmt"
	"os"
	"strings"

	schedvalues "paratech/desktop/internal/domain/schedule/values"
)

// OpenCallLink opens the online call of a lesson (ADR-0032, «Opening and copying»). The canonical form is re-parsed
// right before the start and, like OpenAuthPage, always goes through a temporary .url file: the link never reaches a
// command line. A %NAME% naming an existing environment variable is refused. It returns like OpenLink.
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
	if hasEnvReference(s, environ()) {
		return "", fmt.Errorf("%w: the call link names an environment variable", ErrRefused)
	}
	return s, nil
}

// environ is a seam so tests inject variables.
var environ = os.Environ

// hasEnvReference reports whether s has %NAME% with NAME an existing variable (case-insensitive, as on Windows).
// Valid escapes can spell such a name (%DB_PASSWORD%20), and the shell may expand it when it reads the .url file.
// Every '%' is tried as an opener, so in %A%B% both A and B are checked.
func hasEnvReference(s string, env []string) bool {
	keys := envKeys(env)
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			return false
		}
		name := s[i+1 : i+1+j]
		if !isEnvName(name) {
			continue
		}
		for _, k := range keys {
			if strings.EqualFold(k, name) {
				return true
			}
		}
	}
	return false
}

// envKeys takes the names out of KEY=VALUE entries; Windows keeps hidden "=C:=C:\dir" entries whose key starts with '='.
func envKeys(env []string) []string {
	keys := make([]string, 0, len(env))
	for _, e := range env {
		start := 0
		if strings.HasPrefix(e, "=") {
			start = 1
		}
		if eq := strings.IndexByte(e[start:], '='); eq >= 0 {
			e = e[:start+eq]
		}
		if e != "" {
			keys = append(keys, e)
		}
	}
	return keys
}

func isEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '(' && c != ')' {
			return false
		}
	}
	return true
}
