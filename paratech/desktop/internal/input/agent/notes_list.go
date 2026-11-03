package agent

import (
	"context"

	"paratech/desktop/internal/cases/notes"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// listNotes is list_notes (catalog v10): the active notes of a lesson or of a subject, as the pages read them. A row
// is the note's ID, subject, anchor as it reads now (a recording or lesson that is gone reads as its day), its date and
// counters; never its title, text or file names (ADR-0022). At most values.MaxReadItems rows, as every list.
func (t tools) listNotes(ctx context.Context, call values.Call, _ values.Scope) (values.ToolResult, []values.Ref, error) {
	var (
		views []notes.View
		err   error
	)
	if r, ok := call.Args().ID("lesson"); ok {
		views, err = t.lessonNotes(ctx, shared.LessonID(r.ID()))
	} else {
		s, _ := call.Args().ID("subject")
		views, err = t.actions.notes.ForSubject(ctx, shared.SubjectID(s.ID()))
	}
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	rows := make([]values.NoteRow, 0, len(views))
	for _, v := range views {
		id, ok := refOfID(values.KindNote, string(v.Note.ID()))
		if !ok {
			continue
		}
		row := values.NoteRow{ID: id, Parts: len(v.Note.Parts()), NoTextParts: v.NoTextPages}
		row.Subject, _ = refOfID(values.KindSubject, string(v.Note.Subject()))
		row.Anchor, row.Date = noteAnchorRow(v.Anchor)
		rows = append(rows, row)
	}
	return values.NotesResult(call.ID(), rows)
}

// lessonNotes are the notes of a schedule lesson: those attached to it and, once it is recorded, those attached to
// its recording (the page attaches them there).
func (t tools) lessonNotes(ctx context.Context, id shared.LessonID) ([]notes.View, error) {
	views, err := t.actions.notes.ForLesson(ctx, notes.Lecture{Lesson: id})
	if err != nil {
		return nil, err
	}
	recs, err := t.activity.ListRecordings(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		if r.LessonID != id {
			continue
		}
		more, err := t.actions.notes.ForLesson(ctx, notes.Lecture{Recording: r.ID})
		if err != nil {
			return nil, err
		}
		views = append(views, more...)
	}
	return views, nil
}

// noteAnchorRow is the anchor of a note and its date as a row shows them.
func noteAnchorRow(a note.Anchor) (values.NoteAnchor, values.Date) {
	var d values.Date
	if nd := a.Date(); !nd.IsZero() {
		d, _ = values.NewDate(nd.Year(), nd.Month(), nd.Day())
	}
	switch a.Kind() {
	case note.AnchorRecording:
		return values.NoteAtRecording, d
	case note.AnchorLesson:
		return values.NoteAtLesson, d
	case note.AnchorDay:
		return values.NoteAtDay, d
	}
	return values.NoteUndated, values.Date{}
}
