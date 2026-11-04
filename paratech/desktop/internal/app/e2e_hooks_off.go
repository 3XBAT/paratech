//go:build !e2e

package app

import (
	"log/slog"

	"paratech/desktop/config"
)

// newE2ERecorder has no recorder: without tag e2e the hooks are not linked and the shell keeps the native dialogs
// and openers (ADR-0035 §3).
func newE2ERecorder(config.Config, *slog.Logger) e2eRecorder { return nil }
