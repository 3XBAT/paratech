package app

import (
	"fmt"
	"log/slog"

	"paratech/desktop/internal/adapters/localfile"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/recordingimport"
)

// wireRecordingImport builds the import of old recordings (TASK-161): the picked file is read by localfile, copied by
// the Storage Manager and handed to the transcription enqueuer, so it needs c.transcriptions. No Hub port is given:
// the sound never leaves the computer (hard rule 2). Its sweep of the copies a crash left runs as a startup cleanup.
func (c *core) wireRecordingImport(e env, recordings *sqlite.RecordingRepository, lessons *sqlite.LessonRepository,
	log *slog.Logger) (*recordingimport.Service, error) {
	svc, err := recordingimport.New(recordingimport.Deps{
		Originals: localfile.NewSeekReader(), Store: c.store, Space: c.store, Recordings: recordings, Lessons: lessons,
		Completions: c.transcriptions, Clock: e.clock, IDs: e.ids, Tx: c.db, Log: log,
	})
	if err != nil {
		return nil, fmt.Errorf("app: recording import: %w", err)
	}
	// A failed sweep is only logged by the startup; the next start tries again (TASK-303).
	c.cleanups = append(c.cleanups, cleanup{name: importSweepStep, run: svc.SweepOrphans})
	return svc, nil
}

// importSweepStep names the startup sweep of import copies a crash left without a recording.
const importSweepStep = "import copies without a recording"
