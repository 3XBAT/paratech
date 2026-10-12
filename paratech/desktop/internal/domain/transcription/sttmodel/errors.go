package sttmodel

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

// Errors of the sttmodel package (ARCH-18).
var (
	// ErrInvalidModel is returned by New and Restore for a model that breaks a rule.
	ErrInvalidModel = errors.New("invalid stt model")

	// ErrPinned refuses the removal of a model that an unfinished job pins (PRD section 14 case 11). It wraps
	// shared.ErrConflict: the state of other data blocks the operation.
	ErrPinned = fmt.Errorf("%w: stt model is pinned by an unfinished job", shared.ErrConflict)
)
