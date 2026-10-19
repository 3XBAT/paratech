package values

import "time"

// A silent recording (TASK-082): a track that is silent all or almost all the time, typically because the chosen app
// played nothing. Its transcription would be empty, so it is not transcribed and the user is told.
const (
	// MinAudibleSeconds is the least number of audible seconds of a track that is not silent.
	MinAudibleSeconds = 3
	// minAudiblePercent is the least share of audible seconds, in percent, of a track that is not silent.
	minAudiblePercent = 1
)

// AssembledTrack is what the assembly of a final track measured: its length and how many of its one-second windows
// are audible (a peak of at least HeardLevel). It holds no audio.
type AssembledTrack struct {
	Duration       time.Duration
	Seconds        int // one-second windows; a last partial one counts
	AudibleSeconds int
}

// Silent reports a track that is silent all or almost all the time: fewer than MinAudibleSeconds audible seconds, or
// fewer than 1 % of all.
func (t AssembledTrack) Silent() bool {
	return t.AudibleSeconds < MinAudibleSeconds || t.AudibleSeconds*100 < t.Seconds*minAudiblePercent
}
