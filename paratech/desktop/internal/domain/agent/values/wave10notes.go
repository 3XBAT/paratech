package values

import (
	"fmt"
	"slices"
)

// Results of list_notes and note (ADR-0033 §8, §10): IDs, the anchor and counters only. The title, the text and the
// file names of «Мой конспект» are the student's and never reach the planner (ADR-0022).

// NoteAnchor is what a note is attached to, as the planner sees it.
type NoteAnchor string

// Anchors of a note.
const (
	NoteAtRecording NoteAnchor = "recording"
	NoteAtLesson    NoteAnchor = "lesson"
	NoteAtDay       NoteAnchor = "day"
	NoteUndated     NoteAnchor = "undated"
)

var noteAnchors = []NoteAnchor{NoteAtRecording, NoteAtLesson, NoteAtDay, NoteUndated}

// NoteRow is one note of list_notes: its ID, subject, anchor with its date, how many parts it has and how many of them
// are pages without text.
type NoteRow struct {
	ID          Ref
	Subject     Ref
	Anchor      NoteAnchor
	Date        Date // zero for an undated note
	Parts       int
	NoTextParts int
}

// NotesResult is the result of list_notes and the IDs it shows, at most MaxReadItems rows.
func NotesResult(callID string, rows []NoteRow) (ToolResult, []Ref, error) {
	type item struct {
		ID          string `json:"id"`
		Subject     string `json:"subject,omitempty"`
		Anchor      string `json:"anchor"`
		Date        string `json:"date,omitempty"`
		Parts       int    `json:"parts"`
		NoTextParts int    `json:"parts_without_text"`
	}
	items := make([]item, 0, len(rows))
	refs := make([][]Ref, 0, len(rows)) // per row
	for _, r := range rows {
		if err := errorsOf(wantRef(r.ID, KindNote, true), wantRef(r.Subject, KindSubject, false)); err != nil {
			return ToolResult{}, nil, err
		}
		if !slices.Contains(noteAnchors, r.Anchor) || (r.Anchor == NoteUndated) != r.Date.IsZero() {
			return ToolResult{}, nil, fmt.Errorf("%w: a note anchor with a date unless undated", ErrInvalidResult)
		}
		items = append(items, item{
			ID: r.ID.ID(), Subject: r.Subject.ID(), Anchor: string(r.Anchor), Date: r.Date.String(),
			Parts: max(r.Parts, 0), NoTextParts: min(max(r.NoTextParts, 0), max(r.Parts, 0)),
		})
		refs = append(refs, []Ref{r.ID, r.Subject})
	}
	return listResult(callID, "notes", items, refs)
}

// NoteWriteRow is what note shows the planner: the note it changed (zero for none), whether anything changed, whether
// an add made the note, and the number of its parts after an add (0: not shown).
type NoteWriteRow struct {
	Note    Ref
	Changed bool
	Created bool
	Parts   int
}

// NoteResult is the result of note and the IDs it shows.
func NoteResult(callID string, w NoteWriteRow) (ToolResult, []Ref, error) {
	if err := wantRef(w.Note, KindNote, false); err != nil {
		return ToolResult{}, nil, err
	}
	if w.Created && (!w.Changed || w.Note.IsZero()) {
		return ToolResult{}, nil, fmt.Errorf("%w: a made note changed something and has an id", ErrInvalidResult)
	}
	body := map[string]any{"done": true, "changed": w.Changed}
	var refs []Ref
	if !w.Note.IsZero() {
		body["id"] = w.Note.ID()
		refs = append(refs, w.Note)
	}
	if w.Created {
		body["created"] = true
	}
	if w.Parts > 0 {
		body["parts"] = w.Parts
	}
	r := ToolResult{CallID: callID, ResultJSON: mustJSON(body)}
	if err := r.Validate(); err != nil {
		return ToolResult{}, nil, err
	}
	return r, refs, nil
}
