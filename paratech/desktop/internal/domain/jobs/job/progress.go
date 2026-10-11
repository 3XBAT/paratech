package job

import (
	"fmt"
	"unicode/utf8"
)

// MaxCheckpointBytes bounds the opaque checkpoint of a job (PRD section 7.4). A checkpoint holds resume
// information such as the index of the last finished chunk, never content.
const MaxCheckpointBytes = 4096

// Progress is what a running handler reports to the job runner (PRD sections 7.3 and 7.4): a percentage for the
// UI and an opaque checkpoint from which the job can resume after a restart or when AC power is lost. It is a
// validated value object. The runner, as the only owner of a claimed job, applies it to the aggregate and
// persists it; handlers never save jobs themselves (ADR-0004).
type Progress struct {
	percent    int
	checkpoint string
}

// NewProgress validates and builds a progress report. percent is 0..100. checkpoint is optional opaque text of
// at most MaxCheckpointBytes bytes, valid UTF-8 without NUL; it must not contain secrets or transcript text.
func NewProgress(percent int, checkpoint string) (Progress, error) {
	if percent < 0 || percent > 100 {
		return Progress{}, fmt.Errorf("%w: percent %d is outside 0..100", ErrInvalidProgress, percent)
	}
	if len(checkpoint) > MaxCheckpointBytes {
		return Progress{}, fmt.Errorf("%w: checkpoint is longer than %d bytes", ErrInvalidProgress, MaxCheckpointBytes)
	}
	if !utf8.ValidString(checkpoint) {
		return Progress{}, fmt.Errorf("%w: checkpoint is not valid UTF-8", ErrInvalidProgress)
	}
	for _, r := range checkpoint {
		if r == 0 {
			return Progress{}, fmt.Errorf("%w: checkpoint contains NUL", ErrInvalidProgress)
		}
	}
	return Progress{percent: percent, checkpoint: checkpoint}, nil
}

// Percent returns the completion percentage, 0..100.
func (p Progress) Percent() int { return p.percent }

// Checkpoint returns the opaque resume information; empty means none.
func (p Progress) Checkpoint() string { return p.checkpoint }
