package assignment

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
)

// StudentEdit is what the student changes of an assignment of any origin from the UI (ADR-0029, addendum 2026-10-27
// «Edit after creation»). The texts are the student's words: data, never instructions.
type StudentEdit struct {
	Title       string
	Note        string    // the description; may be empty
	Due         time.Time // zero: no deadline
	SubjectName string    // the library subject's name, stored as the student's subject correction; "": none
}

// EditByStudent stores e: the details, and the subject as the student's correction (the teacher correction stays).
// The rules depend on the origin: an assignment the student added keeps those of NewByStudent (note at most
// MaxNoteRunes, deadline within now - 31 days ... now + 2 years); an extracted one those of NewDetails. Both refuse
// bidi controls in the title and the note. Violations wrap ErrInvalidTitle, ErrInvalidNote or ErrInvalidDue (all
// ErrInvalidDetails) and change nothing; a deleted assignment is ErrDeleted. The assignment is user-edited from then
// on, also when nothing changed: the student has confirmed it.
func (a *Assignment) EditByStudent(e StudentEdit, now time.Time) error {
	if err := a.NotDeleted(); err != nil {
		return err
	}
	if err := checkStudentTitle(e.Title); err != nil {
		return err
	}
	if a.ByStudent() {
		if err := checkStudentNote(e.Note); err != nil {
			return err
		}
		if err := checkStudentDue(e.Due, now); err != nil {
			return err
		}
	} else if err := checkExtractedNote(e.Note); err != nil {
		return err
	}
	d, err := NewDetails(e.Title, e.Note, e.Due)
	if err != nil {
		return err
	}

	a.details, a.userEdited, a.updatedAt = d, true, now
	a.correction = values.NewLessonNames(e.SubjectName, a.correction.Teacher())
	return nil
}

// checkExtractedNote is the description rule of NewDetails (tabs allowed: model text may hold them) plus the bidi ban,
// as a field error.
func checkExtractedNote(note string) error {
	note = strings.TrimSpace(note)
	switch {
	case !utf8.ValidString(note) || utf8.RuneCountInString(note) > MaxDescriptionRunes:
		return fmt.Errorf("%w: must be at most %d characters of valid text", ErrInvalidNote, MaxDescriptionRunes)
	case strings.ContainsFunc(note, func(r rune) bool { return badControl(r) || bidiControl(r) }):
		return fmt.Errorf("%w: contains control characters", ErrInvalidNote)
	}
	return nil
}
