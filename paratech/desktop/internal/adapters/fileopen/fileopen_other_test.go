//go:build !windows

package fileopen

import (
	"context"
	"errors"
	"testing"
)

func TestOpenArtifactIsUnsupported(t *testing.T) {
	root := newRoot(t, "a.docx")
	o := &Opener{root: root, run: noRun(t)}
	if err := o.OpenArtifact(context.Background(), ref(t, "a.docx")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("OpenArtifact = %v, want ErrUnsupported", err)
	}
}
