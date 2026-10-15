package app

import (
	"context"
	"fmt"
	"path/filepath"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/adapters/filestore"
)

// runRecorder is the --recorder run mode: the audio recorder child that the app starts from its own executable
// (ADR-0002 (audio capture)). It opens only the Storage Manager on <DataDir>/artifacts: no instance lock (the parent
// holds it), no database, no UI shell and no jobs. It captures until stdin closes; stdout carries only the protocol
// events, the log goes to stderr. Any exit code but 0 is an error; the parent reads the outcome from the events.
func (a *App) runRecorder(ctx context.Context) error {
	store, err := filestore.New(filepath.Join(a.cfg.DataDir, artifactsDir))
	if err != nil {
		return fmt.Errorf("app: recorder: open artifact store: %w", err)
	}
	defer func() { _ = store.Close() }()

	code := audiocapture.RunChild(ctx, a.env.childStdin, a.env.childStdout, a.env.captureSource, store, a.log,
		audiocapture.ChildConfig{})
	if code != audiocapture.ChildExitOK {
		return fmt.Errorf("app: recorder: exit code %d", code)
	}
	return nil
}
