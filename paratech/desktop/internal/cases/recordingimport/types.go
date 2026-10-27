package recordingimport

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports of the Service; all but Log are required.
type Deps struct {
	Originals   Originals
	Store       ArtifactStore
	Space       DiskSpace
	Recordings  RecordingRepository
	Lessons     LessonReader
	Completions CompletionListener
	Clock       Clock
	IDs         IDGenerator
	Tx          Tx
	Log         *slog.Logger
}

// Request names the file and its lecture. Path is the file the student chose in the open dialog (only read). With
// LessonID the lecture is that past lesson and starts when it started; without one it is StartedAt (required) with the
// Subject, Kind and Title the student named, all optional plain text.
type Request struct {
	Path      string
	LessonID  shared.LessonID
	StartedAt time.Time
	Subject   string
	Kind      string
	Title     string
}

// Result is the recording of the file. Existing: the same bytes were imported before, and that recording is returned
// instead of a new one.
type Result struct {
	Recording *recording.Recording
	Existing  bool
}
