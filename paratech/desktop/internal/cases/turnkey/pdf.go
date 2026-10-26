package turnkey

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// PDFTimeout bounds one print: the renderer notices a crashed browser only at the deadline.
const PDFTimeout = 60 * time.Second

// Log lines of «Скачать PDF»; none holds a text of the work.
const (
	opPDFSaved   = "turnkey: pdf saved"
	opPDFUnfiled = "turnkey: pdf not taken back from «ДЗ»"
)

// ExportPDF is «Скачать PDF» (TASK-198, ADR-0020): the PDF of a done paper or lab report, filed next to its .docx (the
// same «ДЗ», or «Входящие» as the .docx) and kept by the work. It is printed from the same document as the .docx
// (paperDocument) with the year of its title page; the profile is read now, so a name changed since shows in the PDF. A
// PDF of the current content whose material is still there is returned without a new print. A synchronous call, no
// job: a local file without an external side effect, bounded by PDFTimeout. A work that changed during the print
// gets nothing: the PDF goes to «Входящие» and the answer is ErrChanged. Errors: ErrNotConfigured, ErrNotFound,
// ErrWritingRunning, ErrNotWritable (not done, or a presentation), ErrPDFUnsupported, ErrPDFFailed, ErrChanged,
// ErrUnavailable; a cancelled ctx wraps its error.
func (s *Service) ExportPDF(ctx context.Context, id shared.WorkID) (PDFSaved, error) {
	if s.writing == nil || s.writing.PDF == nil {
		return PDFSaved{}, ErrNotConfigured
	}
	w, err := s.get(ctx, id)
	if err != nil {
		return PDFSaved{}, err
	}
	switch {
	case w.Status() == work.StatusWriting:
		return PDFSaved{}, ErrWritingRunning
	case w.Status() != work.StatusDone:
		return PDFSaved{}, fmt.Errorf("%w: the work is %s", ErrNotWritable, w.Status())
	case w.Presentation():
		return PDFSaved{}, fmt.Errorf("%w: a presentation has no pdf", ErrNotWritable)
	}
	if p, ok := w.PDF(); ok {
		there, err := s.materialThere(ctx, p.Material)
		switch {
		case err != nil:
			return PDFSaved{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		case there:
			return PDFSaved{View: s.view(ctx, w), Material: p.Material}, nil
		}
	}

	student, err := s.student(ctx)
	if err != nil {
		return PDFSaved{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	year := w.PaperYear(s.clock.Now().Location())
	doc := paperDocument(w, student, year)
	if err := doc.Validate(); err != nil {
		return PDFSaved{}, fmt.Errorf("%w: %w", ErrPDFFailed, err)
	}
	pctx, cancel := context.WithTimeout(ctx, PDFTimeout)
	data, err := s.writing.PDF.RenderPDF(pctx, doc)
	cancel()
	if err != nil {
		return PDFSaved{}, pdfError(err)
	}

	wr, _ := w.Writing()
	brief, _ := w.Brief()
	m, err := s.writing.Shelf.FileHomework(ctx, wr.Snapshot.Subject, homeworkdoc.PaperPDFName(work.TypeName(brief.WorkType), wr.Topic), data)
	if err != nil {
		return PDFSaved{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	ref, err := material.BlobRef(m.Digest())
	if err != nil {
		return PDFSaved{}, s.dropPDF(ctx, m.ID(), fmt.Errorf("%w: %w", ErrUnavailable, err))
	}
	f := work.File{Material: m.ID(), Artifact: ref.String(), Content: w.ContentKey(), Year: year}
	if err := w.AttachPDF(f, s.clock.Now()); err != nil {
		return PDFSaved{}, s.dropPDF(ctx, m.ID(), fmt.Errorf("%w: %w", ErrUnavailable, err))
	}
	err = s.works.Save(ctx, w)
	switch {
	case errors.Is(err, shared.ErrVersionConflict):
		// A second click may have kept this very file (the same bytes are one material): then it is the answer.
		if cur, gerr := s.get(ctx, id); gerr == nil {
			if p, ok := cur.PDF(); ok && p.Material == m.ID() {
				return PDFSaved{View: s.view(ctx, cur), Material: m.ID()}, nil
			}
		}
		return PDFSaved{}, s.dropPDF(ctx, m.ID(), fmt.Errorf("%w: %w", ErrChanged, err))
	case err != nil:
		return PDFSaved{}, s.dropPDF(ctx, m.ID(), fmt.Errorf("%w: %w", ErrUnavailable, err))
	}
	s.log.InfoContext(ctx, opPDFSaved, slog.String("work", string(id)), slog.String("material", string(m.ID())))
	return PDFSaved{View: s.view(ctx, w), Material: m.ID()}, nil
}

// pdfError is the error of a print: the caller's cancel stays it, no PDF on this computer is ErrPDFUnsupported, any
// other failure (a timeout too) ErrPDFFailed.
func pdfError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	case errors.Is(err, errors.ErrUnsupported):
		return fmt.Errorf("%w: %w", ErrPDFUnsupported, err)
	}
	return fmt.Errorf("%w: %w", ErrPDFFailed, err)
}

// dropPDF takes a filed PDF the work does not keep back to «Входящие» (the file kept), so «ДЗ» shows no stray copy,
// and returns cause. A failure to move it is logged only.
func (s *Service) dropPDF(ctx context.Context, id shared.MaterialID, cause error) error {
	if err := s.unfile(ctx, id); err != nil {
		s.log.WarnContext(ctx, opPDFUnfiled, slog.String("material", string(id)), slog.Any("cause", err))
	}
	return cause
}

// materialThere reports whether material id is still in the library (not removed); it may be in any folder.
func (s *Service) materialThere(ctx context.Context, id shared.MaterialID) (bool, error) {
	m, _, err := s.materials.MaterialText(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return false, nil
	case m == nil && err != nil:
		return false, err
	}
	return m != nil && m.Active(), nil
}
