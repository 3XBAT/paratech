//go:build !windows

package wincred

import (
	"context"
	"errors"
	"testing"
)

// Off Windows the store fails loudly instead of keeping secrets somewhere weaker.
func TestUnsupportedOffWindows(t *testing.T) {
	ctx := context.Background()
	s, err := New("mail")
	if err != nil {
		t.Fatal(err)
	}

	calls := map[string]func() error{
		"set":    func() error { return s.Set(ctx, "k", "v") },
		"get":    func() error { _, err := s.Get(ctx, "k"); return err },
		"exists": func() error { _, err := s.Exists(ctx, "k"); return err },
		"delete": func() error { return s.Delete(ctx, "k") },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrUnsupported) || !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s = %v, want ErrUnsupported", name, err)
		}
	}
}
