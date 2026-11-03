package sqlite

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
)

// Stored form of the wave-10 compensations of the assignment tools (TASK-320) and of note (TASK-321). The other
// wave-10 kinds come with their dispatch tasks.

// assignmentFieldsWire is action.AssignmentFields: the student's own words, stored as typed, never logged.
type assignmentFieldsWire struct {
	Title       string `json:"title"`
	Note        string `json:"note,omitempty"`
	Due         string `json:"due,omitempty"` // RFC 3339 in UTC; "": none
	SubjectName string `json:"subject_name,omitempty"`
}

// doneMarkWire is one action.DoneMark.
type doneMarkWire struct {
	Assignment string `json:"assignment"`
	WasDone    bool   `json:"was_done"`
	Version    int64  `json:"version"`
}

// noteAnchorWire is action.NoteAnchor: its kind, the recording or lesson of that kind, and the date.
type noteAnchorWire struct {
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	Date   string `json:"date,omitempty"` // YYYY-MM-DD; "": undated
}

// noteAnchorTarget is the ID kind of the target of an anchor kind; "" for one without a target.
func noteAnchorTarget(k action.NoteAnchorKind) values.IDKind {
	switch k {
	case action.AnchorRecording:
		return values.KindRecording
	case action.AnchorLesson:
		return values.KindLesson
	}
	return ""
}

func anchorWireOf(a action.NoteAnchor) *noteAnchorWire {
	return &noteAnchorWire{Kind: string(a.Kind), Target: a.Target.ID(), Date: a.Date.String()}
}

func anchorOfWire(w *noteAnchorWire) (action.NoteAnchor, error) {
	if w == nil {
		return action.NoteAnchor{}, fmt.Errorf("compensation %s without its anchor", action.CompReanchorNote)
	}
	a := action.NoteAnchor{Kind: action.NoteAnchorKind(w.Kind)}
	if kind := noteAnchorTarget(a.Kind); kind != "" {
		a.Target, _ = optionalRef(kind, w.Target) // a malformed id stays zero and fails the check in Restore
	}
	if w.Date != "" {
		d, err := values.ParseDate(w.Date)
		if err != nil {
			return action.NoteAnchor{}, err
		}
		a.Date = d
	}
	return a, nil
}

func wireOf10(c action.Compensation, w compensationWire) (compensationWire, error) {
	switch v := c.(type) {
	case action.RevertAssignment:
		w.Assignment = v.Assignment.ID()
		f := assignmentFieldsWire{Title: v.Prior.Title, Note: v.Prior.Note, SubjectName: v.Prior.SubjectName}
		if !v.Prior.Due.IsZero() {
			f.Due = v.Prior.Due.UTC().Format(time.RFC3339Nano)
		}
		w.Fields = &f
	case action.RestoreAssignment:
		w.Assignment = v.Assignment.ID()
	case action.DeleteAssignment:
		w.Assignment = v.Assignment.ID()
	case action.RestoreDone:
		for _, m := range v.Marks() {
			w.Marks = append(w.Marks, doneMarkWire{Assignment: m.Assignment.ID(), WasDone: m.WasDone, Version: m.Version})
		}
	case action.RemoveNotePart:
		w.Note, w.Material = v.Note.ID(), v.Part.ID()
	case action.RemoveNote:
		w.Note = v.Note.ID()
	case action.ReanchorNote:
		w.Note, w.Anchor = v.Note.ID(), anchorWireOf(v.Prior)
	case action.RetitleNote:
		w.Note, w.OldTitle = v.Note.ID(), v.OldTitle
	case action.RestoreNote:
		w.Note = v.Note.ID()
	default:
		return compensationWire{}, fmt.Errorf("unknown compensation %s", c.Kind())
	}
	return w, nil
}

func compensationOfWire10(w compensationWire) (action.Compensation, error) {
	ref := func(id string) values.Ref {
		r, _ := optionalRef(values.KindAssignment, id) // a malformed id stays zero and fails the check in Restore
		return r
	}
	switch action.CompensationKind(w.Kind) {
	case action.CompRevertAssignment:
		if w.Fields == nil {
			return nil, fmt.Errorf("compensation %s without its fields", w.Kind)
		}
		f := action.AssignmentFields{Title: w.Fields.Title, Note: w.Fields.Note, SubjectName: w.Fields.SubjectName}
		if w.Fields.Due != "" {
			d, err := time.Parse(time.RFC3339Nano, w.Fields.Due)
			if err != nil {
				return nil, err
			}
			f.Due = d.UTC()
		}
		return action.RevertAssignment{Assignment: ref(w.Assignment), Prior: f}, nil
	case action.CompRestoreAssignment:
		return action.RestoreAssignment{Assignment: ref(w.Assignment)}, nil
	case action.CompDeleteAssignment:
		return action.DeleteAssignment{Assignment: ref(w.Assignment)}, nil
	case action.CompRestoreDone:
		marks := make([]action.DoneMark, 0, len(w.Marks))
		for _, m := range w.Marks {
			marks = append(marks, action.DoneMark{Assignment: ref(m.Assignment), WasDone: m.WasDone, Version: m.Version})
		}
		return action.NewRestoreDone(marks)
	}
	note := func() values.Ref {
		r, _ := optionalRef(values.KindNote, w.Note)
		return r
	}
	switch action.CompensationKind(w.Kind) {
	case action.CompRemoveNotePart:
		part, _ := optionalRef(values.KindMaterial, w.Material)
		return action.RemoveNotePart{Note: note(), Part: part}, nil
	case action.CompRemoveNote:
		return action.RemoveNote{Note: note()}, nil
	case action.CompReanchorNote:
		a, err := anchorOfWire(w.Anchor)
		if err != nil {
			return nil, err
		}
		return action.ReanchorNote{Note: note(), Prior: a}, nil
	case action.CompRetitleNote:
		return action.RetitleNote{Note: note(), OldTitle: w.OldTitle}, nil
	case action.CompRestoreNote:
		return action.RestoreNote{Note: note()}, nil
	}
	return nil, fmt.Errorf("unknown compensation kind %q", w.Kind)
}
