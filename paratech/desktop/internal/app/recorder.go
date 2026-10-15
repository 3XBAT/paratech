package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/cases/recordingsession"
)

// platformRecorder is the audio recorder of this build: on Windows the recorder child of this executable
// (--recorder --data-dir dataDir) that writes chunks through the Storage Manager on <dataDir>/artifacts; elsewhere
// none (the product targets Windows only). It starts nothing.
func platformRecorder(dataDir string, files *filestore.Store, log *slog.Logger) (recordingsession.Recorder, error) {
	if !audiocapture.Supported() {
		return audiocapture.Unavailable{}, nil
	}
	launcher, err := audiocapture.ChildLauncher(dataDir)
	if err != nil {
		return nil, err
	}
	rec, err := audiocapture.New(audiocapture.Deps{
		Files: files, Launcher: launcher, Processes: audiocapture.PlatformProcesses(), Log: log,
	}, audiocapture.DefaultConfig())
	if err != nil {
		return nil, err
	}
	return rec, nil
}

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
