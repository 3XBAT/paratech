package homeworkflow

import (
	"bytes"
	"context"
	"fmt"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, used in error messages.
const (
	opDocxName   = "homeworkflow: name the word file"
	opExportDocx = "homeworkflow: export the word file"
	opSaveDocx   = "homeworkflow: save the word file"
)

// DocxName returns the file name the save dialog proposes for the Word file of assignment a (homeworkdoc.FileName). A
// missing assignment wraps shared.ErrNotFound.
func (s *Drafts) DocxName(ctx context.Context, a shared.AssignmentID) (string, error) {
	as, err := s.assignments.Get(ctx, a)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", opDocxName, a, err)
	}
	return homeworkdoc.FileName(as.Details().Title()), nil
}

// ExportDocx writes version n of the draft of assignment a as a Word file with the title page (TASK-094) to the Word
// export of the assignment (proposal.ExportRefOf with proposal.FormatDocx), the file a send in that format attaches.
// Like ExportDraft it is a user action only, it is refused with ErrExportLocked while an email of the assignment is
// unresolved, and a send confirmed for the earlier content becomes STALE. A missing assignment, draft or version wraps
// shared.ErrNotFound and nothing is written.
func (s *Drafts) ExportDocx(ctx context.Context, a shared.AssignmentID, n int) (DraftExport, error) {
	content, err := s.docx(ctx, a, n)
	if err != nil {
		return DraftExport{}, fmt.Errorf("%s %s: %w", opExportDocx, a, err)
	}
	locked, err := s.deliveries.HasUnresolved(ctx, a)
	if err != nil {
		return DraftExport{}, fmt.Errorf("%s %s: %w", opExportDocx, a, err)
	}
	if locked {
		return DraftExport{}, fmt.Errorf("%s %s: %w", opExportDocx, a, ErrExportLocked)
	}
	ref, err := proposal.ExportRefOf(a, proposal.FormatDocx)
	if err != nil {
		return DraftExport{}, fmt.Errorf("%s %s: %w", opExportDocx, a, err)
	}
	if err := s.artifacts.Write(ctx, ref, bytes.NewReader(content)); err != nil {
		return DraftExport{}, fmt.Errorf("%s %s: %w", opExportDocx, a, err)
	}
	return DraftExport{Version: n, File: ref}, nil
}

// SaveDocx writes version n of the draft of assignment a as a Word file with the title page to path, which the student
// chose in the save dialog (never the model). The file is written atomically by the DocumentWriter; it is not the
// attachment of a send, so no unresolved email locks it. A missing assignment, draft or version wraps
// shared.ErrNotFound; an empty path is ErrNoPath. Nothing is written on an error.
func (s *Drafts) SaveDocx(ctx context.Context, a shared.AssignmentID, n int, path string) error {
	if path == "" {
		return fmt.Errorf("%s %s: %w", opSaveDocx, a, ErrNoPath)
	}
	content, err := s.docx(ctx, a, n)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveDocx, a, err)
	}
	if err := s.files.WriteFile(ctx, path, content); err != nil {
		return fmt.Errorf("%s %s: %w", opSaveDocx, a, err)
	}
	return nil
}

// docx renders version n: the title page from the student profile and the lesson of the assignment (the subject and
// the teacher it shows, values.LessonLabel), the year of the clock in the student's zone, then the draft text.
func (s *Drafts) docx(ctx context.Context, a shared.AssignmentID, n int) ([]byte, error) {
	_, r, err := s.revision(ctx, a, n)
	if err != nil {
		return nil, err
	}
	as, err := s.assignments.Get(ctx, a)
	if err != nil {
		return nil, err
	}
	label, err := s.lessons.label(ctx, as)
	if err != nil {
		return nil, err
	}
	st, err := s.profiles.Load(ctx)
	if err != nil {
		return nil, err
	}
	p := st.Profile().Fields()
	doc := homeworkdoc.Build(homeworkdoc.Input{
		Student: homeworkdoc.Student{
			FullName: p.FullName, Group: p.Group, University: p.University, Faculty: p.Faculty, City: p.City,
		},
		Subject: label.Subject(), Teacher: label.Teacher(), AssignmentTitle: as.Details().Title(),
		DraftTitle: r.Title(), DraftBody: r.Body(), Year: s.clock.Now().Year(),
	})
	return s.renderer.Render(ctx, doc)
}
