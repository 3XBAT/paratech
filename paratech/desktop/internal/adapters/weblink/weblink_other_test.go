//go:build !windows

package weblink

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/transcription/values"
)

func TestOpenLinkIsUnsupported(t *testing.T) {
	u, err := values.ParseSourceURL("https://huggingface.co/ggerganov/whisper.cpp")
	if err != nil {
		t.Fatal(err)
	}
	if err := New().OpenLink(context.Background(), u); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("OpenLink = %v, want ErrUnsupported", err)
	}
}
