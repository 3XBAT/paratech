//go:build !e2e

package app

import (
	"log/slog"
	"testing"
)

// A build without tag e2e has no recorder even for an E2E config (config never sets one there): the hooks are not
// linked (ADR-0035 §3).
func TestE2ERecorderNotBuilt(t *testing.T) {
	d := newE2EDirs(t)
	if r := newE2ERecorder(d.config(false), slog.New(slog.DiscardHandler)); r != nil {
		t.Fatalf("recorder %T without tag e2e, want none", r)
	}
}
