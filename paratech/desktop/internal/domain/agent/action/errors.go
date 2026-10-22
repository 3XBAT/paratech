package action

import "errors"

// Errors of the aggregate.
var (
	// ErrInvalidAction is returned by the constructors and Restore for data that breaks an invariant.
	ErrInvalidAction = errors.New("invalid agent action")

	// ErrInvalidTransition is returned for a status change the transition table does not allow.
	ErrInvalidTransition = errors.New("invalid agent action transition")

	// ErrInvalidCompensation is returned for a compensation that is malformed or does not match the tool.
	ErrInvalidCompensation = errors.New("invalid compensation")

	// ErrTargetChanged means the target was changed after the action; Undo is refused («Изменено после — отменить
	// нельзя»).
	ErrTargetChanged = errors.New("target changed since the action")
)
