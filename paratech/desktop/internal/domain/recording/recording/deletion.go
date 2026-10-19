package recording

import (
	"fmt"
	"slices"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// MaxDeletedAssignments bounds the found homework deleted together with one recording.
const MaxDeletedAssignments = 256

// CheckDeletable refuses deletion while the recording is captured or finalized (ADR-0004(a)).
func (r *Recording) CheckDeletable() error {
	if r.status.InProgress() {
		return fmt.Errorf("%w: %s", ErrNotDeletable, r.status)
	}
	return nil
}

// MarkDeleting marks the recording for deletion together with the found homework assignments (TASK-095, ADR-0018).
// The mark is stored before any file or row is removed: from then on the recording is hidden, never transcribed or
// assembled, and the deletion is resumed after a crash. A recording in progress is refused with ErrNotDeletable, one
// already marked with ErrDeleting (its first choice of assignments stays).
func (r *Recording) MarkDeleting(now time.Time, assignments []shared.AssignmentID) error {
	if err := r.CheckDeletable(); err != nil {
		return err
	}
	if r.Deleting() {
		return fmt.Errorf("%w: %s", ErrDeleting, r.id)
	}
	if now.IsZero() {
		return fmt.Errorf("%w: no deletion time", ErrInvalidRecording)
	}
	if err := checkDeletedAssignments(assignments); err != nil {
		return err
	}

	r.deletingAt, r.deletingAssignments, r.updatedAt = now, slices.Clone(assignments), now
	return nil
}

// Deleting reports whether the recording is marked for deletion.
func (r *Recording) Deleting() bool { return !r.deletingAt.IsZero() }

// DeletingAt returns when the recording was marked for deletion; zero when it is not.
func (r *Recording) DeletingAt() time.Time { return r.deletingAt }

// DeletingAssignments returns the found homework the user chose to delete with the recording.
func (r *Recording) DeletingAssignments() []shared.AssignmentID {
	return slices.Clone(r.deletingAssignments)
}

// HasSound reports whether the recording holds audio files: the final track or chunks written so far.
func (r *Recording) HasSound() bool { return !r.audio.IsZero() || r.chunks > 0 }

// checkDeletedAssignments validates the ids: well formed, distinct and at most MaxDeletedAssignments.
func checkDeletedAssignments(ids []shared.AssignmentID) error {
	if len(ids) > MaxDeletedAssignments {
		return fmt.Errorf("%w: %d assignments to delete, at most %d", ErrInvalidRecording, len(ids), MaxDeletedAssignments)
	}
	seen := make(map[shared.AssignmentID]bool, len(ids))
	for _, id := range ids {
		if err := id.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidRecording, err)
		}
		if seen[id] {
			return fmt.Errorf("%w: assignment %s listed twice", ErrInvalidRecording, id)
		}
		seen[id] = true
	}
	return nil
}
