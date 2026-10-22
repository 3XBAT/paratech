package turnkey

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// WorkRepository stores the works. Implemented by the sqlite adapter.
type WorkRepository interface {
	// Get loads a work; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.WorkID) (*work.Work, error)

	// ActiveOf loads the work of a source that has not failed; none wraps shared.ErrNotFound.
	ActiveOf(ctx context.Context, src work.Source) (*work.Work, error)

	// Save writes it under its version: a stale one wraps shared.ErrVersionConflict, a second active work of the
	// same source shared.ErrDuplicate, a source that does not exist shared.ErrConflict.
	Save(ctx context.Context, w *work.Work) error

	// Delete removes it if it is still at version expected, else shared.ErrVersionConflict.
	Delete(ctx context.Context, id shared.WorkID, expected int64) error
}

// Assignments reads the assignment a work is made from. Implemented by the sqlite adapter.
type Assignments interface {
	// Get loads an assignment; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.AssignmentID) (*assignment.Assignment, error)
}

// MaterialTexts reads the text of a library material (cases/library MaterialText, wired by the app). Errors wrap the
// domain sentinels: shared.ErrNotFound, material.ErrRemoved, material.ErrTextNotExtracted (PDF), material.ErrDamaged,
// material.ErrTextUnreadable; anything else is a local failure.
type MaterialTexts interface {
	MaterialText(ctx context.Context, id shared.MaterialID) (*material.Material, material.Text, error)
}

// Reader is the quarantined reader TRIAGE_ASSIGNMENT on the Hub (ADR-0022). Implemented by the hubclient adapter. The
// output is untrusted model text: only work.Request.Parse reads it. Errors follow the Hub client:
// shared.ErrQuotaExceeded; work.ErrInvalidAnswer for any permanent provider error (the Hub's charged schema refusal
// or an uncharged model refusal, both one used attempt); anything else (no network, no Hub, a refused key) uses no
// attempt, and the same key is asked again later for free.
type Reader interface {
	TriageAssignment(ctx context.Context, req work.Request) (string, error)
}

// StepAdvisor answers «Спросить по шагу» from the narrow context of one helper step (work.StepQuestion), with no
// tools. No implementation exists yet: the Hub has no operation for it (ADR-0025); without one AskStep answers
// ErrNotConfigured. The answer is untrusted text for a card only.
type StepAdvisor interface {
	AdviseStep(ctx context.Context, q work.StepQuestion) (string, error)
}

// Clock supplies the current time (ARCH-24).
type Clock interface {
	Now() time.Time
}

// IDs generates the identifiers of new works (ARCH-24).
type IDs interface {
	NewWorkID() shared.WorkID
}
