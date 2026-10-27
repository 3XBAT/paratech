package knowledgebase

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log of the student's notes. Neither note texts nor titles are logged.
const (
	opNotesUnreadable = "knowledge base: notes not readable"
	opNotesNotListed  = "knowledge base: notes not listed, recorded lessons only"
	opNoteSubjects    = "knowledge base: subjects of the notes not listed, recorded lessons only"
	opNoteSubject     = "knowledge base: note of an unknown subject left out"
	opNoteLesson      = "knowledge base: lesson of a note not read"
	opNotePart        = "knowledge base: part of a note not read"
	opNotePartSubject = "knowledge base: part of a note of another subject left out"
)

// withNotes loads every lesson of the knowledge base: the recorded ones with the student's notes joined and the
// lectures known only from notes (kb.WithNotes). Removed notes are left out, as are rows that cannot be read (logged).
func (s *Service) withNotes(ctx context.Context) ([]kb.Lesson, error) {
	recorded, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	if s.notes == nil {
		return recorded, nil
	}
	facts, err := s.noteFacts(ctx, recorded)
	if err != nil {
		return nil, err
	}
	return kb.WithNotes(recorded, facts), nil
}

// noteFacts gathers what kb.WithNotes needs of the active notes. A lesson anchor not met by a recording is looked up
// in the schedule: a lesson gone falls back to its day; a failed read keeps the lesson. Notes or subjects that cannot
// be listed give no facts (notesFailed); only a cancelled read fails.
func (s *Service) noteFacts(ctx context.Context, recorded []kb.Lesson) ([]kb.NoteFacts, error) {
	notes, unreadable, err := s.notes.ListAll(ctx)
	if err != nil {
		return nil, s.notesFailed(ctx, opNotesNotListed, err)
	}
	if len(unreadable) > 0 {
		s.log.Warn(opNotesUnreadable, slog.Int("count", len(unreadable)), slog.String("first", string(unreadable[0])))
	}
	if len(notes) == 0 {
		return nil, nil
	}

	subs, err := s.subjects.Subjects(ctx)
	if err != nil {
		return nil, s.notesFailed(ctx, opNoteSubjects, err)
	}
	names := make(map[shared.SubjectID]string, len(subs))
	for _, sub := range subs {
		names[sub.ID()] = sub.Name().String()
	}
	recordedLessons := map[shared.LessonID]bool{}
	for _, l := range recorded {
		if l.ScheduleLesson != "" {
			recordedLessons[l.ScheduleLesson] = true
		}
	}

	loc := s.clock.Now().Location()
	out := make([]kb.NoteFacts, 0, len(notes))
	for _, n := range notes {
		if !n.Active() {
			continue
		}
		name, ok := names[n.Subject()]
		if !ok {
			s.log.Warn(opNoteSubject, slog.String("note", string(n.ID())), slog.String("subject", string(n.Subject())))
			continue
		}
		a := n.Anchor()
		f := kb.NoteFacts{
			Note: n.ID(), Title: n.Title(), Subject: kb.LibrarySubject{ID: n.Subject(), Name: name},
			Anchor: kb.NoteAnchor(a.Kind()), Recording: a.Recording(), Lesson: a.Lesson(), Day: dayOf(a.Date(), loc),
			Kind: a.LessonKind(), LessonTitle: a.Title(), Parts: n.Parts(), CreatedAt: n.CreatedAt(),
		}
		if a.Kind() == note.AnchorLesson && !recordedLessons[a.Lesson()] {
			if f.LessonGone, err = s.lessonGone(ctx, n.ID(), a.Lesson()); err != nil {
				return nil, err
			}
		}
		out = append(out, f)
	}
	return out, nil
}

// notesFailed isolates a failed read of the notes or of the subjects: it is logged (cause only) and the knowledge base
// keeps its recorded lessons, so a broken notes table does not hide them. Only a cancelled read is returned.
func (s *Service) notesFailed(ctx context.Context, op string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.log.Warn(op, slog.Any("cause", err))
	return nil
}

// lessonGone reports that the schedule no longer has the lesson of a note. A failed read is logged and keeps the
// lesson; a cancelled one is returned.
func (s *Service) lessonGone(ctx context.Context, id shared.NoteID, lesson shared.LessonID) (bool, error) {
	_, err := s.schedule.Get(ctx, lesson)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, shared.ErrNotFound):
		return true, nil
	case ctx.Err() != nil:
		return false, ctx.Err()
	}
	s.log.Warn(opNoteLesson, slog.String("note", string(id)), slog.Any("cause", err))
	return false, nil
}

// noteFragments reads the text of the parts of a note, in order. A removed material and one without text (a PDF, a
// scan) are left out; a part that cannot be read, or that is a material of another subject than the note's, is left
// out and logged (ADR-0019). Only a cancelled read fails.
func (s *Service) noteFragments(ctx context.Context, n kb.Note) ([]kb.NoteFragment, error) {
	var out []kb.NoteFragment
	for _, id := range n.Parts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m, text, err := s.noteTexts.MaterialText(ctx, id)
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.Is(err, material.ErrRemoved), errors.Is(err, material.ErrTextNotExtracted):
			continue
		default:
			s.log.Warn(opNotePart, slog.String("note", string(n.ID)), slog.String("material", string(id)),
				slog.Any("cause", err))
			continue
		}
		if m == nil || m.Subject() != n.Subject || !m.Active() {
			s.log.Warn(opNotePartSubject, slog.String("note", string(n.ID)), slog.String("material", string(id)))
			continue
		}
		for _, f := range text.Fragments() {
			out = append(out, kb.NoteFragment{
				Note: n.ID, Material: id, Locator: kb.Locator{Kind: string(f.Locator.Kind), N: f.Locator.N},
				Text: strings.Join(f.Lines, "\n"),
			})
		}
	}
	return out, nil
}

// dayOf is the local midnight of a note date; zero for no date.
func dayOf(d note.Date, loc *time.Location) time.Time {
	if d.IsZero() {
		return time.Time{}
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}
