package sqlite

import (
	"context"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the note repository, used in its error messages.
const (
	opGetNote     = "sqlite: get note"
	opListNotes   = "sqlite: list notes"
	opSaveNote    = "sqlite: save note"
	opRestoreNote = "stored note"
)

// NoteRepository stores the student's own notes with their ordered parts (TASK-149, ADR-0027). Validation stays in
// note.Note: the repository only converts rows.
type NoteRepository struct{ db *DB }

// NewNoteRepository returns the note repository of db.
func NewNoteRepository(db *DB) *NoteRepository { return &NoteRepository{db: db} }

// Get loads a note; a missing one wraps shared.ErrNotFound.
func (r *NoteRepository) Get(ctx context.Context, id shared.NoteID) (*note.Note, error) {
	q := r.db.Queries(ctx)
	row, err := q.GetNote(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetNote, id, MapError(ctx, err))
	}
	return noteWithParts(ctx, q, row)
}

// ListBySubject returns the notes of subject in any status, by the date of the anchor (undated last), then as made.
func (r *NoteRepository) ListBySubject(ctx context.Context, sub shared.SubjectID) ([]*note.Note, error) {
	q := r.db.Queries(ctx)
	rows, err := q.ListNotesOfSubject(ctx, string(sub))
	return notesWithParts(ctx, q, rows, err)
}

// ListAll returns every note in any status, ordered as ListBySubject, for the knowledge base. A row that cannot be
// restored is left out and its id goes to unreadable, so one broken row does not hide the others.
func (r *NoteRepository) ListAll(ctx context.Context) (notes []*note.Note, unreadable []shared.NoteID, err error) {
	q := r.db.Queries(ctx)
	rows, err := q.ListNotes(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", opListNotes, MapError(ctx, err))
	}
	notes = make([]*note.Note, 0, len(rows))
	for _, row := range rows {
		parts, err := q.ListNoteParts(ctx, row.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("%s %s: %w", opGetNote, row.ID, MapError(ctx, err))
		}
		n, err := restoreNote(row, parts)
		if err != nil {
			unreadable = append(unreadable, shared.NoteID(row.ID))
			continue
		}
		notes = append(notes, n)
	}
	return notes, unreadable, nil
}

// ListByRecording returns the notes anchored to the recording in any status, as made. The recording may be gone: the
// anchor has no foreign key.
func (r *NoteRepository) ListByRecording(ctx context.Context, rec shared.RecordingID) ([]*note.Note, error) {
	q := r.db.Queries(ctx)
	rows, err := q.ListNotesOfRecording(ctx, jobNullString(string(rec)))
	return notesWithParts(ctx, q, rows, err)
}

// ListByLesson returns the notes anchored to the schedule lesson in any status, as made. The lesson may be gone: the
// anchor has no foreign key.
func (r *NoteRepository) ListByLesson(ctx context.Context, lesson shared.LessonID) ([]*note.Note, error) {
	q := r.db.Queries(ctx)
	rows, err := q.ListNotesOfLesson(ctx, jobNullString(string(lesson)))
	return notesWithParts(ctx, q, rows, err)
}

// Save writes the note and its parts in one transaction: an INSERT for version 0, otherwise a version compare-and-swap
// (ADR-0004(d)); the parts are rewritten in order. An id that exists or a stale version wraps
// shared.ErrVersionConflict; a subject or material that does not exist shared.ErrConflict.
func (r *NoteRepository) Save(ctx context.Context, n *note.Note) error {
	st := n.State()
	if st.Version < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveNote, st.Version)
	}
	a := st.Anchor
	recording, lesson := jobNullString(string(a.Recording())), jobNullString(string(a.Lesson()))
	date := jobNullString(a.Date().String())
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			if err := DuplicateAsVersionConflict(MapError(ctx, q.InsertNote(ctx, sqlcgen.InsertNoteParams{
				ID: string(st.ID), SubjectID: string(st.Subject), Title: st.Title, AnchorKind: string(a.Kind()),
				RecordingID: recording, LessonID: lesson, AnchorDate: date, LessonKind: a.LessonKind(),
				LessonTitle: a.Title(), Status: string(st.Status), CreatedAt: jobMs(st.CreatedAt),
				UpdatedAt: jobMs(st.UpdatedAt),
			}))); err != nil {
				return err
			}
		} else {
			rows, err := q.UpdateNote(ctx, sqlcgen.UpdateNoteParams{
				Title: st.Title, AnchorKind: string(a.Kind()), RecordingID: recording, LessonID: lesson, AnchorDate: date,
				LessonKind: a.LessonKind(), LessonTitle: a.Title(), Status: string(st.Status),
				UpdatedAt: jobMs(st.UpdatedAt), ID: string(st.ID), ExpectedVersion: st.Version,
			})
			if err := VersionCAS(ctx, rows, err); err != nil {
				return err
			}
			if err := q.DeleteNoteParts(ctx, string(st.ID)); err != nil {
				return MapError(ctx, err)
			}
		}
		for i, m := range st.Parts {
			if err := q.InsertNotePart(ctx, sqlcgen.InsertNotePartParams{
				NoteID: string(st.ID), MaterialID: string(m), Position: int64(i),
			}); err != nil {
				return MapError(ctx, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveNote, st.ID, err)
	}
	n.Persisted(st.Version + 1)
	return nil
}

func notesWithParts(ctx context.Context, q *sqlcgen.Queries, rows []sqlcgen.Note, err error) ([]*note.Note, error) {
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListNotes, MapError(ctx, err))
	}
	out := make([]*note.Note, 0, len(rows))
	for _, row := range rows {
		n, err := noteWithParts(ctx, q, row)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func noteWithParts(ctx context.Context, q *sqlcgen.Queries, row sqlcgen.Note) (*note.Note, error) {
	parts, err := q.ListNoteParts(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetNote, row.ID, MapError(ctx, err))
	}
	return restoreNote(row, parts)
}

func restoreNote(row sqlcgen.Note, parts []string) (*note.Note, error) {
	anchor, err := noteAnchor(row)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opRestoreNote, row.ID, err)
	}
	st := note.State{
		Version: row.Version, ID: shared.NoteID(row.ID), Subject: shared.SubjectID(row.SubjectID), Anchor: anchor,
		Title: row.Title, Status: note.Status(row.Status), CreatedAt: jobFromMs(row.CreatedAt),
		UpdatedAt: jobFromMs(row.UpdatedAt),
	}
	for _, p := range parts {
		st.Parts = append(st.Parts, shared.MaterialID(p))
	}
	n, err := note.Restore(st)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opRestoreNote, row.ID, err)
	}
	return n, nil
}

func noteAnchor(row sqlcgen.Note) (note.Anchor, error) {
	var date note.Date
	if row.AnchorDate.Valid {
		d, err := note.ParseDate(row.AnchorDate.String)
		if err != nil {
			return note.Anchor{}, err
		}
		date = d
	}
	return note.NewAnchor(note.AnchorSpec{
		Kind: note.AnchorKind(row.AnchorKind), Recording: shared.RecordingID(row.RecordingID.String),
		Lesson: shared.LessonID(row.LessonID.String), Date: date, LessonKind: row.LessonKind, Title: row.LessonTitle,
	})
}
