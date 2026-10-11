package transcript

import "paratech/desktop/internal/domain/shared"

// Transcript is the text result of transcribing one recording with a pinned model version
// (PRD section 8, AC-08). Skeleton: see the package comment.
type Transcript struct {
	id        shared.TranscriptID
	recording shared.RecordingID // reference by ID only (ARCH-13)
}

// ID returns the transcript identifier.
func (t *Transcript) ID() shared.TranscriptID { return t.id }

// RecordingID returns the recording this transcript was produced from.
func (t *Transcript) RecordingID() shared.RecordingID { return t.recording }
