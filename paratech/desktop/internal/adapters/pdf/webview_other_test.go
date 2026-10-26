//go:build !windows

package pdf

import (
	"errors"
	"testing"
)

func TestPrintUnsupportedOutsideWindows(t *testing.T) {
	tests := []struct {
		name string
		html []byte
	}{
		{name: "page", html: []byte("<p>Привет</p>")},
		{name: "empty", html: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPrinter(t.TempDir(), nil)

			out, err := p.Print(t.Context(), tt.html)

			if !errors.Is(err, ErrUnsupported) || out != nil {
				t.Fatalf("Print = %d bytes, %v; want ErrUnsupported", len(out), err)
			}
		})
	}
}
