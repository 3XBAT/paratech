package app

import (
	"context"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/scheduler"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// promptRecordings is the Recordings port of the recording prompt over the recording session; pure delegation.
type promptRecordings struct {
	session *recordingsession.Service
}

// Ready is true once the startup recovery has marked the interrupted recordings. The lifecycle calls Recover only
// after the startup cleanups, so this also means the cleanups ran (ADR-0009). It is also true once the session is
// closed (a failed start or the shutdown), so a start fails instead of waiting for a recovery that never comes.
func (p promptRecordings) Ready() bool {
	select {
	case <-p.session.RecoveryMarked():
		return true
	default:
		return false
	}
}

// StartRecording starts a recording of source for the lesson, or without one.
func (p promptRecordings) StartRecording(ctx context.Context, lesson shared.LessonID, source values.Source) (shared.RecordingID, error) {
	info, err := p.session.Start(ctx, recordingsession.StartRequest{LessonID: lesson, Source: source})
	return info.ID, err
}

// wirePrompts builds the recording prompt on popup and the scheduler that feeds it. UI mode only: headless has no
// popup, and a scheduler there would mark the announcements as made without showing anything.
func (c *core) wirePrompts(e env, popup recordingprompt.Popup, log *slog.Logger) error {
	prompts, err := recordingprompt.New(recordingprompt.Deps{
		Settings: sqlite.NewSettingsRepository(c.db), Apps: e.apps(log), Popup: popup,
		Recordings: promptRecordings{session: c.recordings}, Clock: e.clock, Sleeper: e.clock, Log: log,
	}, recordingprompt.DefaultConfig())
	if err != nil {
		return fmt.Errorf("app: recording prompt: %w", err)
	}
	sched, err := scheduler.New(scheduler.Deps{
		Lessons: sqlite.NewLessonRepository(c.db), Clock: e.clock, Sleeper: e.clock, Events: prompts, Log: log,
	}, scheduler.DefaultConfig())
	if err != nil {
		return fmt.Errorf("app: scheduler: %w", err)
	}

	c.prompts, c.scheduler = prompts, sched
	c.ui.Prompt = prompts
	return nil
}

// loops are the background loops of the core: the scheduler and the recording prompt, when they are wired.
func (c *core) loops() []loop {
	if c.prompts == nil {
		return nil
	}
	return []loop{{name: "scheduler", run: c.scheduler.Run}, {name: "recording prompt", run: c.prompts.Run}}
}
