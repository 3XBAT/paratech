package transcriptview

import (
	"io"
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// State is where the transcript of a recording stands.
type State string

// States of a transcript.
const (
	// StateNone: no recognition was started (no model or speech component yet, or the recording is not complete).
	StateNone State = "none"
	// StateWaiting: the recognition waits in the queue, for a retry or for AC power.
	StateWaiting State = "waiting"
	// StateRecognising: the recognition runs; Status.Percent is its progress.
	StateRecognising State = "recognising"
	// StateDone: the transcript is stored.
	StateDone State = "done"
	// StateFailed: the recognition failed or was cancelled; parts recognised before stay visible.
	StateFailed State = "failed"
)

// Paging limits of Segments.
const (
	// MaxPage bounds one page; a three-hour lecture has about 4000 segments.
	MaxPage = 1000

	// cachedDocuments is how many parsed documents stay in memory for paging.
	cachedDocuments = 4
)

// Status is the head of the transcript viewer of one recording.
type Status struct {
	Recording shared.RecordingID
	// Title is the lesson title, or "Запись <date>, <time>" for a recording without a lesson.
	Title     string
	StartedAt time.Time
	State     State
	// Percent is the progress of a running or waiting recognition, 0 otherwise.
	Percent int
	// Segments is how many segments Segments can page through.
	Segments int
	// Final: the segments come from the stored transcript; otherwise they are the live parts recognised so far.
	Final bool
	// Playable: the recording has its final track (OpenAudio works).
	Playable bool
}

// Page is a part of the segments, starting at index From of Total.
type Page struct {
	From     int
	Total    int
	Final    bool
	Segments []transcript.Segment
}

// Audio is the final track of a recording for playback; the caller closes Reader.
type Audio struct {
	Reader io.ReadSeekCloser
	Size   int64
}

// Deps are the ports the Service needs; Log is optional.
type Deps struct {
	Recordings  RecordingReader
	Lessons     LessonReader
	Transcripts TranscriptFinder
	Jobs        JobFinder
	Live        LiveParts
	Artifacts   ArtifactStore
	Writer      TextWriter
	Clock       Clock
	Log         *slog.Logger
}
