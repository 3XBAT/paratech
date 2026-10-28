package thread

import (
	"errors"

	"paratech/desktop/internal/domain/shared"
)

// ForkedFrom returns the chat this one was forked from (ADR-0028 «Restart from an earlier message»); empty when it is
// not a fork. Informational only: the source may be gone, nothing follows the link.
func (th *Thread) ForkedFrom() shared.ThreadID { return th.forkedFrom }

// checkFork is the Restore check of the fork source: a valid id that is not the chat's own.
func checkFork(s State) error {
	if s.ForkedFrom == "" {
		return nil
	}
	if err := s.ForkedFrom.Validate(); err != nil {
		return err
	}
	if s.ForkedFrom == s.ID {
		return errors.New("a chat forked from itself")
	}
	return nil
}
