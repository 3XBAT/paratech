package deletion

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

// Errors of the deletion service (ARCH-18).
var (
	// ErrUnknownAssignment means a chosen assignment is not one found in the transcripts of the recording.
	ErrUnknownAssignment = errors.New("the assignment was not found in this recording")
)

// Class tells how an assignment found in a recording is treated by its deletion.
type Class string

// Classes of a found assignment.
const (
	// ClassPlain is deleted when chosen; the page offers it checked.
	ClassPlain Class = "plain"
	// ClassDraft has a homework draft: deleted only when chosen explicitly; the page shows it apart, unchecked.
	ClassDraft Class = "draft"
	// ClassEmails has email history: never deleted, it stays in the homework list without its recording.
	ClassEmails Class = "emails"
)

// Assignment is what the deletion knows of one homework assignment found in the recording.
type Assignment struct {
	ID         shared.AssignmentID
	Title      string // model output, as stored: shown as plain text only
	HasDraft   bool
	Deliveries int // emails of the assignment in any status
}

// Class returns how the deletion treats the assignment.
func (a Assignment) Class() Class {
	switch {
	case a.Deliveries > 0:
		return ClassEmails
	case a.HasDraft:
		return ClassDraft
	}
	return ClassPlain
}

// Contents is what a recording holds that its deletion removes.
type Contents struct {
	Sound        bool // the final track or the chunks written so far
	Transcripts  int
	Parts        int // segments of a recognition in progress
	Summaries    int
	LectureTexts int
	Jobs         int // unfinished jobs of the recording, cancelled first
	Assignments  []Assignment
}

// CheckChoice checks the assignments the user chose to delete with the recording: each must be one found in it. An
// assignment with email history may be chosen; it is kept (Deletable leaves it out).
func CheckChoice(found []Assignment, chosen []shared.AssignmentID) error {
	known := make(map[shared.AssignmentID]bool, len(found))
	for _, a := range found {
		known[a.ID] = true
	}
	for _, id := range chosen {
		if !known[id] {
			return fmt.Errorf("%w: %s", ErrUnknownAssignment, id)
		}
	}
	return nil
}

// Deletable returns the chosen assignments that are deleted now, in the order of found: chosen, still found in the
// recording, and without email history. The rest of the found ones stay, detached from the recording.
func Deletable(found []Assignment, chosen []shared.AssignmentID) []shared.AssignmentID {
	want := make(map[shared.AssignmentID]bool, len(chosen))
	for _, id := range chosen {
		want[id] = true
	}
	out := []shared.AssignmentID{}
	for _, a := range found {
		if want[a.ID] && a.Class() != ClassEmails {
			out = append(out, a.ID)
			want[a.ID] = false // a duplicate row is listed once
		}
	}
	return out
}

// Kept returns the found assignments that stay without the recording (those not in deleted), in found order, each
// once.
func Kept(found []Assignment, deleted []shared.AssignmentID) []shared.AssignmentID {
	gone := make(map[shared.AssignmentID]bool, len(deleted))
	for _, id := range deleted {
		gone[id] = true
	}
	out := []shared.AssignmentID{}
	for _, a := range found {
		if !gone[a.ID] {
			out = append(out, a.ID)
			gone[a.ID] = true
		}
	}
	return out
}
