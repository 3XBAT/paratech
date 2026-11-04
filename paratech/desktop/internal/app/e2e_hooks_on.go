//go:build e2e

package app

import (
	"log/slog"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/e2ehooks"
)

// newE2ERecorder is the e2ehooks adapter on the control dir of an E2E run; nil outside the mode. The only file of the
// app that imports e2ehooks: a build without tag e2e does not link it (ADR-0035 §3).
func newE2ERecorder(cfg config.Config, log *slog.Logger) e2eRecorder {
	if !cfg.E2E.On() {
		return nil
	}
	return e2ehooks.New(cfg.E2E.Dir, log)
}
