package recordingsession

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/limits"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports the Service needs. All are required except Log.
type Deps struct {
	Recordings  RecordingRepository
	Transcripts RecordingTranscripts
	Recorder    Recorder
	Artifacts   ArtifactStore
	Tx          Tx
	Clock       Clock
	Sleeper     Sleeper
	IDs         IDGenerator
	Completions CompletionListener
	Log         *slog.Logger // optional: nil discards
}

// DefaultHeartbeatInterval is how often the session of a running capture stores its liveness mark (PRD section 6.2).
const DefaultHeartbeatInterval = 15 * time.Second

// Config tunes the Service.
type Config struct {
	MaxDuration       time.Duration // hard cap of a recording; validated by the limits policy
	TrackExtension    string        // extension of the final track recordings/<id>.<ext>
	HeartbeatInterval time.Duration // how often a running capture stores its heartbeat; positive
}

// DefaultConfig returns the 3 hour cap, a WAV track and a heartbeat every 15 seconds.
func DefaultConfig() Config {
	return Config{MaxDuration: limits.DefaultMaxDuration, TrackExtension: "wav", HeartbeatInterval: DefaultHeartbeatInterval}
}

// StartRequest is what the popup or the UI asks for.
type StartRequest struct {
	LessonID shared.LessonID // optional: empty for an ad-hoc recording
	Source   values.Source   // what to capture; the zero value is the whole computer
}

// Info is the state of a recording as the UI shows it.
type Info struct {
	ID        shared.RecordingID
	LessonID  shared.LessonID
	Status    recording.Status
	StartedAt time.Time
	StoppedAt time.Time     // zero while capturing
	Deadline  time.Time     // when the hard cap finalizes the recording
	Duration  time.Duration // length of the final track; zero until COMPLETED
	Chunks    int
	Problem   string // short reason of a failure or of an early end
}

func infoOf(r *recording.Recording) Info {
	d, _ := r.Duration()
	return Info{
		ID: r.ID(), LessonID: r.LessonID(), Status: r.Status(), StartedAt: r.StartedAt(), StoppedAt: r.StoppedAt(),
		Deadline: r.Deadline(), Duration: d, Chunks: r.Chunks(), Problem: r.Problem(),
	}
}
