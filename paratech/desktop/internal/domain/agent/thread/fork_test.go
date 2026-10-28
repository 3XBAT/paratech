package thread

import (
	"errors"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

// The fork source is restored as stored; a new chat is not a fork; a bad source is refused.
func TestRestoreForkedFrom(t *testing.T) {
	if got := chatOf(t, 1).ForkedFrom(); got != "" {
		t.Fatalf("new chat forked from %q", got)
	}
	for _, tt := range []struct {
		name string
		from shared.ThreadID
		ok   bool
	}{
		{"not a fork", "", true},
		{"another chat", "th-source", true},
		{"itself", "th1", false},
		{"bad id", "a/b", false},
		{"too long", shared.ThreadID(strings.Repeat("a", 200)), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := chatOf(t, 1).State()
			s.Version, s.ForkedFrom = 1, tt.from
			th, err := Restore(s)
			if !tt.ok {
				if !errors.Is(err, ErrInvalidThread) {
					t.Fatalf("Restore = %v, want ErrInvalidThread", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if th.ForkedFrom() != tt.from || th.State().ForkedFrom != tt.from {
				t.Fatalf("ForkedFrom = %q, state %q; want %q", th.ForkedFrom(), th.State().ForkedFrom, tt.from)
			}
		})
	}
}
