package filestore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

const opExport = "export homework"

// ErrZeroCommand is returned for a zero export command, which no confirmed proposal produces.
var ErrZeroCommand = errors.New("zero export command")

// ExportHomework writes the content of a confirmed export_homework proposal to its export file, atomically (Write).
// The content is the one rendering of the command (values.HomeworkMarkdown); the same command writes the same file
// again. A target that is not the export file of the command's assignment (proposal.ExportRef) is refused with
// shared.ErrInvalidArtifactRef before anything is written, so no other artifact can be overwritten.
func (s *Store) ExportHomework(ctx context.Context, cmd proposal.ExportCommand) error {
	if cmd.IsZero() {
		return fmt.Errorf("%s: %w", opExport, ErrZeroCommand)
	}

	want, err := proposal.ExportRef(cmd.AssignmentID())
	if err != nil {
		return fmt.Errorf("%s: %w", opExport, err)
	}
	if cmd.Target() != want {
		return fmt.Errorf("%s: %w: %s is not the export file of %s", opExport, shared.ErrInvalidArtifactRef,
			cmd.Target(), cmd.AssignmentID())
	}

	return s.Write(ctx, want, strings.NewReader(cmd.Content()))
}
