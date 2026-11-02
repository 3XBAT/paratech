package sqlite

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
)

// Stored form of the wave-10 compensations of the assignment tools (TASK-320). The other wave-10 kinds come with
// their dispatch tasks.

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
	return nil, fmt.Errorf("unknown compensation kind %q", w.Kind)
}
