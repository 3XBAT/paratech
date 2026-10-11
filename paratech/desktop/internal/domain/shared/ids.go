package shared

import (
	"fmt"
	"unicode"
)

// maxIDLen bounds identifier length; identifiers are opaque strings produced by the IDGenerator ports.
const maxIDLen = 128

func validateID(kind, id string) error {
	if id == "" {
		return fmt.Errorf("%w: empty %s id", ErrInvalidID, kind)
	}
	if len(id) > maxIDLen {
		return fmt.Errorf("%w: %s id is longer than %d bytes", ErrInvalidID, kind, maxIDLen)
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("%w: %s id contains whitespace or control characters", ErrInvalidID, kind)
		}
	}
	return nil
}

// RecordingID identifies a recording (PRD section 6.2).
type RecordingID string

// String returns the identifier as a plain string.
func (id RecordingID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable).
func (id RecordingID) Validate() error { return validateID("recording", string(id)) }

// JobID identifies a persistent job (PRD section 7.1).
type JobID string

// String returns the identifier as a plain string.
func (id JobID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable).
func (id JobID) Validate() error { return validateID("job", string(id)) }

// TranscriptID identifies a transcript (PRD section 8, AC-08).
type TranscriptID string

// String returns the identifier as a plain string.
func (id TranscriptID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable).
func (id TranscriptID) Validate() error { return validateID("transcript", string(id)) }

// SttModelID identifies an installed speech-to-text model (PRD section 8).
type SttModelID string

// String returns the identifier as a plain string.
func (id SttModelID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable).
func (id SttModelID) Validate() error { return validateID("stt model", string(id)) }

// LessonID identifies a schedule entry, a lesson (PRD section 11).
type LessonID string

// String returns the identifier as a plain string.
func (id LessonID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable).
func (id LessonID) Validate() error { return validateID("lesson", string(id)) }

// AssignmentID identifies a homework assignment (PRD section 10).
type AssignmentID string

// String returns the identifier as a plain string.
func (id AssignmentID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable).
func (id AssignmentID) Validate() error { return validateID("assignment", string(id)) }

// DeliveryID identifies an attempt to send an artifact to a recipient (PRD section 10).
type DeliveryID string

// String returns the identifier as a plain string.
func (id DeliveryID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable).
func (id DeliveryID) Validate() error { return validateID("delivery", string(id)) }
