package transcribe

import (
	"log/slog"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/transcription/values"
)

// JobType is the job type the Service handles. The app registers it with the job runner as Heavy (PRD section 7.4,
// AC-21) with the requeue recovery policy: a transcription has no external side effect and a rerun is idempotent.
const JobType = job.Type(values.STTJobType)

// Progress scale of one attempt: the engine run fills 0..engineShare, the stored document is reported at
// documentStored with the checkpoint CheckpointTranscribed; the runner sets 100 on completion.
const (
	engineShare    = 90
	documentStored = 95
)

// CheckpointTranscribed is the checkpoint reported once the document is stored. whisper-cli cannot resume inside an
// audio file, so this is the only safe point: a later attempt that finds the document skips the engine.
const CheckpointTranscribed = "transcribed"

// Deps are the ports the Service needs. Log is optional.
type Deps struct {
	Engine      STTEngine
	Transcripts TranscriptRepository
	Recordings  RecordingReader
	Models      ModelReader
	Artifacts   ArtifactStore
	Events      EventPublisher
	Clock       Clock
	Log         *slog.Logger
}
