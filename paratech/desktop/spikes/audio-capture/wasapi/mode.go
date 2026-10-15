package wasapi

import (
	"errors"
	"fmt"
)

// Mode selects what is captured.
type Mode int

const (
	// ModeInclude is process loopback of a process and its descendants (PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE).
	ModeInclude Mode = iota + 1

	// ModeExclude is process loopback of everything except a process tree (PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE).
	ModeExclude

	// ModeSystem is classic loopback of the default render endpoint (AUDCLNT_STREAMFLAGS_LOOPBACK).
	ModeSystem
)

func (m Mode) String() string {
	switch m {
	case ModeInclude:
		return "include"
	case ModeExclude:
		return "exclude"
	case ModeSystem:
		return "system"
	}
	return fmt.Sprintf("mode(%d)", int(m))
}

// ErrInvalidSpec means a Spec cannot be opened as given.
var ErrInvalidSpec = errors.New("wasapi: invalid capture spec")

// Spec is a mode with its target process (process loopback only).
type Spec struct {
	Mode Mode
	PID  uint32
}

// Validate checks that process loopback has a target and classic loopback has none.
func (s Spec) Validate() error {
	switch s.Mode {
	case ModeInclude, ModeExclude:
		if s.PID == 0 {
			return fmt.Errorf("%w: %s needs a target process", ErrInvalidSpec, s.Mode)
		}
	case ModeSystem:
		if s.PID != 0 {
			return fmt.Errorf("%w: system loopback takes no target process", ErrInvalidSpec)
		}
	default:
		return fmt.Errorf("%w: unknown %s", ErrInvalidSpec, s.Mode)
	}
	return nil
}

func (s Spec) String() string {
	if s.Mode == ModeSystem {
		return s.Mode.String()
	}
	return fmt.Sprintf("%s(pid=%d)", s.Mode, s.PID)
}

// processLoopbackMode maps a mode to PROCESS_LOOPBACK_MODE (audioclientactivationparams.h; values documented by
// Microsoft, the header is not in mingw-w64).
func (m Mode) processLoopbackMode() (uint32, bool) {
	switch m {
	case ModeInclude:
		return processLoopbackModeInclude, true
	case ModeExclude:
		return processLoopbackModeExclude, true
	}
	return 0, false
}

const (
	activationTypeProcessLoopback = 1 // AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK
	processLoopbackModeInclude    = 0 // PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE
	processLoopbackModeExclude    = 1 // PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE
)
