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
// the sound never leaves the computer (hard rule 2).
func (c *core) wireRecordingImport(e env, recordings *sqlite.RecordingRepository, lessons *sqlite.LessonRepository,
	log *slog.Logger) (*recordingimport.Service, error) {
	svc, err := recordingimport.New(recordingimport.Deps{
		Originals: localfile.NewSeekReader(), Store: c.store, Space: c.store, Recordings: recordings, Lessons: lessons,
		Completions: c.transcriptions, Clock: e.clock, IDs: e.ids, Tx: c.db, Log: log,
	})
	if err != nil {
		return nil, fmt.Errorf("app: recording import: %w", err)
	}
	return svc, nil
}
