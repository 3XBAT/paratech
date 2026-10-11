package sttmodel

import "paratech/desktop/internal/domain/shared"

// Model is an installed speech-to-text model (PRD section 8). Skeleton: see the package comment.
type Model struct {
	id shared.SttModelID
}

// ID returns the model identifier.
func (m *Model) ID() shared.SttModelID { return m.id }
