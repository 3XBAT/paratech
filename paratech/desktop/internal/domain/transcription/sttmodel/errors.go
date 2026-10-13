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

	// ErrFileInUse refuses a model whose file is, contains or lies inside the file of another installed model: removing
	// one would destroy the other. It wraps shared.ErrDuplicate.
	ErrFileInUse = fmt.Errorf("%w: model file belongs to another installed model", shared.ErrDuplicate)
)
