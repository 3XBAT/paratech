package work

import (
	"fmt"
	"time"
)

// AttachPDF keeps the PDF of a done paper or lab report (TASK-198, ADR-0020): it is printed from the same document as
// the .docx, so f.Content must be ContentKey and f.Year the year of its title page. A PDF attached again replaces the
// one before (the use case prints anew only when that material is gone). A file stored before TASK-198 has no year: it
// takes the year of its first PDF, so every later PDF has the same title page. A presentation has no PDF. Errors:
// ErrInvalidTransition outside StatusDone or for a presentation, ErrStaleStep for a PDF of other content,
// ErrInvalidWork for a bad file.
func (w *Work) AttachPDF(f File, now time.Time) error {
	switch {
	case w.status != StatusDone || w.writing == nil || w.writing.File == nil:
		return fmt.Errorf("%w: a pdf of a work in %s", ErrInvalidTransition, w.status)
	case w.Presentation():
		return fmt.Errorf("%w: a presentation has no pdf", ErrInvalidTransition)
	case f.Content != w.ContentKey():
		return ErrStaleStep
	}
	next := w.clone()
	p := f
	next.writing.PDF = &p
	if next.writing.File.Year == 0 {
		next.writing.File.Year = f.Year
	}
	return w.move(next, StatusDone, now)
}

// PDF returns the PDF of the content of a done work; ok is false before AttachPDF and from the moment the work is
// written again (its PDF then is a file of the earlier generation only, see Files).
func (w *Work) PDF() (File, bool) {
	if w.status != StatusDone || w.writing == nil || w.writing.PDF == nil || w.writing.PDF.Content != w.ContentKey() {
		return File{}, false
	}
	return *w.writing.PDF, true
}

// PaperYear is the year of the title page of the paper: the one its file was built with, for a file stored before
// TASK-198 the year (in loc) the work last changed, which for a done work is when it was finished unless a PDF was
// attached since. 0 without a file.
func (w *Work) PaperYear(loc *time.Location) int {
	switch {
	case w.writing == nil || w.writing.File == nil:
		return 0
	case w.writing.File.Year > 0:
		return w.writing.File.Year
	case loc == nil:
		return w.updatedAt.Year()
	}
	return w.updatedAt.In(loc).Year()
}
