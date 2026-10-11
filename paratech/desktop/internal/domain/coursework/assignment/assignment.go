package assignment

import "paratech/desktop/internal/domain/shared"

// Assignment is a homework item (PRD section 10). Skeleton: see the package comment.
type Assignment struct {
	id shared.AssignmentID
}

// ID returns the assignment identifier.
func (a *Assignment) ID() shared.AssignmentID { return a.id }
