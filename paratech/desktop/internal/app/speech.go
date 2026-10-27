package app

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"paratech/desktop/internal/adapters/componentstore"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/whispercli"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/speechcomponent"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/transcription/values"
)

// speechFile is the pinned speech component manifest (values.ParseSpeechComponent): the official whisper.cpp release
// archive and the files taken from it (docs/refs/whisper.md, "Speech recognition component"). The build scripts read
// the same file.
const speechFile = "speechcomponent.json"

// speechFiles is read-only embedded data fixed at build time, not mutable state (ARCH-53); go:embed requires a
// package-level variable.
//
//go:embed speechcomponent.json
var speechFiles embed.FS

// embeddedSpeechManifest returns the manifest shipped with the binary.
func embeddedSpeechManifest() []byte {
	data, err := speechFiles.ReadFile(speechFile)
	if err != nil {
		return nil // unreachable: the file is embedded at build time; parsing then fails the start
	}
	return data
}

// speechEnv is where the speech component comes from: the pinned manifest, the folder of the executable that the
// installer fills, and the source of the in-app install.
type speechEnv struct {
	manifest   []byte
	bundledDir string // "" when the executable cannot be found
	source     speechcomponent.ArchiveSource
}

// executableDir is the folder of this program; "" when it cannot be found.
func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// wireSpeech builds the speech component use case on the component folder of dataDir. sttDeps are the transcription
// ports without the engine, which the activator builds from the verified folder.
func (c *core) wireSpeech(dataDir string, e env, sttDeps transcribe.Deps, log *slog.Logger) error {
	spec, err := values.ParseSpeechComponent(e.speech.manifest)
	if err != nil {
		return fmt.Errorf("app: speech component manifest: %w", err)
	}
	c.components, err = componentstore.New(dataDir, e.speech.bundledDir)
	if err != nil {
		return fmt.Errorf("app: speech component folder: %w", err)
	}
	act := &speechActivator{
		runner: c.runner, store: c.store, spec: spec, root: filepath.Join(dataDir, artifactsDir), stt: sttDeps, log: log,
		dirs: map[values.ComponentFolder]string{
			values.FolderBundled:   e.speech.bundledDir,
			values.FolderInstalled: componentstore.InstalledDir(dataDir),
		},
	}
	c.speech, err = speechcomponent.New(speechcomponent.Deps{
		Spec: spec, Source: e.speech.source, Space: c.store, Files: c.components, Activator: act, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: speech component: %w", err)
	}

	// An install writes only after the leftovers of an interrupted one are gone.
	c.cleanups = append(c.cleanups,
		cleanup{name: "speech component leftovers", run: c.components.Cleanup},
		cleanup{name: "speech component installs allowed", run: func(context.Context) (int, error) {
			c.speech.MarkReady()
			return 0, nil
		}})
	return nil
}

// detectSpeech activates speech recognition from a verified folder at start. Without one the stt type stays
// unregistered, STT jobs wait QUEUED, and the pages offer the install.
func (c *core) detectSpeech(ctx context.Context, log *slog.Logger) {
	st, err := c.speech.Detect(ctx)
	if err != nil {
		log.Error("checking the speech component failed", "err", err)
	}
	if st.State != speechcomponent.StateReady {
		log.Warn("speech recognition is not available: whisper-cli and its libraries are missing or do not verify",
			"state", string(st.State))
		log.Warn("transcription jobs wait in the queue until the speech component is installed")
	}
}

// closeSpeech stops a running install within timeout.
func (c *core) closeSpeech(timeout time.Duration) error {
	if c.speech == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.speech.Close(ctx)
}

// speechActivator builds whisper-cli on a verified folder and registers the stt job type, at start before the runner
// starts or at run time after an install (jobrunner.Service.Activate).
type speechActivator struct {
	runner *jobrunner.Service
	store  *filestore.Store
	spec   values.SpeechComponent
	dirs   map[values.ComponentFolder]string
	root   string // the artifact root: the data root of whispercli
	stt    transcribe.Deps
	log    *slog.Logger
}

// Activate implements speechcomponent.Activator. whispercli.New only checks the path: nothing runs here.
func (a *speechActivator) Activate(ctx context.Context, folder values.ComponentFolder) error {
	dir := a.dirs[folder]
	if dir == "" {
		return fmt.Errorf("app: no directory for the %s speech component folder", folder)
	}
	var vad string
	if name := a.spec.VADModel(); name != "" {
		vad = filepath.Join(dir, name)
	}
	engine, err := whispercli.New(whispercli.Config{
		Binary: filepath.Join(dir, a.spec.Executable()), DataRoot: a.root, WorkDir: filepath.Join(a.root, sttWorkDir),
		LogicalCPUs: logicalCPUs(), VADModel: vad, Log: a.log,
	}, a.store, a.store)
	if err != nil {
		return fmt.Errorf("app: speech recognition engine: %w", err)
	}
	// No STT job runs yet: the type is registered below. Errors only leave old run directories behind.
	if n, err := engine.CleanupWork(ctx); err != nil {
		a.log.Warn("removing old speech recognition work directories failed", "err", err)
	} else if n > 0 {
		a.log.Info("startup cleanup", "step", "speech recognition work directories", "removed", n)
	}

	deps := a.stt
	deps.Engine = engine
	stt, err := transcribe.New(deps)
	if err != nil {
		return fmt.Errorf("app: transcription: %w", err)
	}
	rep, err := a.runner.Activate(ctx, sttRegistration(stt))
	if errors.Is(err, jobrunner.ErrAlreadyRegistered) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("app: register stt jobs: %w", err)
	}
	for _, e := range rep.Errors {
		a.log.Warn("stale job left for the next start", "job", e.Job.String(), "type", string(e.Type), "err", e.Err)
	}
	a.log.Info("speech recognition engine ready", "folder", folder.String())
	return nil
}
