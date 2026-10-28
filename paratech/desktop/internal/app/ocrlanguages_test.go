package app

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"paratech/desktop/internal/adapters/winocr"
	"paratech/desktop/internal/cases/preferences"
)

var _ preferences.RecognitionLanguages = ocrLanguages{}

func TestHasRussian(t *testing.T) {
	tests := []struct {
		tags []string
		want bool
	}{
		{tags: nil, want: false},
		{tags: []string{"en-US"}, want: false},
		{tags: []string{"en-US", "ru"}, want: true},
		{tags: []string{"ru-RU"}, want: true},
		{tags: []string{"RU-ru"}, want: true},
		{tags: []string{"rue"}, want: false}, // another language that starts with "ru"
		{tags: []string{"run-BI"}, want: false},
	}
	for _, tt := range tests {
		if got := hasRussian(tt.tags); got != tt.want {
			t.Errorf("hasRussian(%v) = %v, want %v", tt.tags, got, tt.want)
		}
	}
}

// Off Windows the check is unsupported, which the use case shows as an unknown language. On Windows it only reads
// the installed recognizers: any answer is fine, a failure must be one of the adapter's errors.
func TestOCRLanguages(t *testing.T) {
	_, err := newOCRLanguages().RussianAvailable(context.Background())
	if runtime.GOOS != "windows" {
		if !errors.Is(err, winocr.ErrUnsupported) {
			t.Fatalf("off Windows: err = %v, want ErrUnsupported", err)
		}
		return
	}
	if err != nil && !errors.Is(err, winocr.ErrUnsupported) && !errors.Is(err, winocr.ErrEngine) && !errors.Is(err, winocr.ErrTimeout) {
		t.Fatalf("on Windows: err = %v, want nil or an adapter error", err)
	}
}
