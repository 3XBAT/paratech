package action

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/agent/values"
)

// Compensations of the wave-10 tools (ADR-0033 §10).
//
// Trust (hard rule 3; supervisor decision of TASK-310/314): what an Undo writes back — prior values, prior states,
// expected versions — comes from the store, read by the case before the use case runs (Intent.Prior) or returned by
// the use case (its result and the action journal), never from the arguments of the call. The types hold plain
// strings and refs, no model value type (values.Label, values.Query, values.Text, values.Args), and the kinds whose
// prior is known before the use case runs (priorPinned) must equal the Intent.Prior journaled then.
const (
	CompRevertAssignment  CompensationKind = "revert_assignment"  // change_assignment{edit}: the prior fields
	CompRestoreAssignment CompensationKind = "restore_assignment" // change_assignment{delete}: Restore (ADR-0029)
	CompDeleteAssignment  CompensationKind = "delete_assignment"  // change_assignment{restore}: soft delete again
	CompRestoreDone       CompensationKind = "restore_done"       // mark_assignments_done: each prior state
	CompRemoveNotePart    CompensationKind = "remove_note_part"   // note{add_*} into an existing note
	CompRemoveNote        CompensationKind = "remove_note"        // note{add_*} that made a new note: soft remove
	CompReanchorNote      CompensationKind = "reanchor_note"      // note{move}: the prior anchor
	CompRetitleNote       CompensationKind = "retitle_note"       // note{rename}: the prior title
	CompRestoreNote       CompensationKind = "restore_note"       // note{remove}: restore
	CompRestoreSetting    CompensationKind = "restore_setting"    // set_setting: the prior value
	CompStopRecording     CompensationKind = "stop_recording"     // recording{start}: stop, the audio kept
)

// noUndoTools are the U tools one of whose actions completes without a compensation: recording{stop} (ADR-0033 §2,
// О-7 а) and a recording{start} that found one running. Policy marks the call (actionpolicy.Decision.NoUndo); the entry is DONE without «Отменить».
var noUndoTools = map[values.Tool]bool{values.ToolRecording: true}

// priorPinned are the kinds whose whole prior state is read before the use case runs: Complete and Restore accept
// only the compensation the intent journaled as its Prior.
var priorPinned = map[CompensationKind]bool{
	CompRevertAssignment: true, CompReanchorNote: true, CompRetitleNote: true, CompRestoreSetting: true,
}

// checkTrusted ties a compensation to what the journal recorded: a pinned kind equals Intent.Prior, and a list of
// marks expects the version Complete was given.
func checkTrusted(in Intent, c Compensation, targetVersion int64) error {
	if priorPinned[c.Kind()] && (in.Prior == nil || in.Prior != c) {
		return fmt.Errorf("%w: %s restores only the prior state journaled before the action", ErrInvalidCompensation, c.Kind())
	}
	if d, ok := c.(RestoreDone); ok && d.ExpectedVersion() != targetVersion {
		return fmt.Errorf("%w: a list of marks expects its highest version", ErrInvalidCompensation)
	}
	return nil
}

// Bounds of stored prior values (local, never sent to the planner).
const (
	maxPriorTitleRunes = 200   // assignment.MaxTitleRunes
	maxPriorNoteRunes  = 16000 // assignment.MaxDescriptionRunes
	maxNoteTitleRunes  = 120   // note.MaxTitleRunes
)

// AssignmentFields are the fields change_assignment{edit} may change, as stored before the edit.
type AssignmentFields struct {
	Title       string    // never empty
	Note        string    // the description; may be empty
	Due         time.Time // zero: none; with its time of day (D7)
	SubjectName string    // the student's subject correction; "": none
}

func (f AssignmentFields) validate() error {
	if strings.TrimSpace(f.Title) == "" || !oneLine(f.Title, maxPriorTitleRunes) {
		return fmt.Errorf("%w: prior title is not one line of text", ErrInvalidCompensation)
	}
	if !textBlock(f.Note, maxPriorNoteRunes) {
		return fmt.Errorf("%w: prior note is not text", ErrInvalidCompensation)
	}
	if len(f.SubjectName) > maxOldSubject || !oneLine(f.SubjectName, maxOldSubject) {
		return fmt.Errorf("%w: prior subject is not one line of text", ErrInvalidCompensation)
	}
	return nil
}

// RevertAssignment undoes change_assignment{edit}: the fields go back to Prior while the assignment is at the version
// the edit left (the action's TargetVersion). Prior is the stored assignment read before the edit (Intent.Prior).
type RevertAssignment struct {
	Assignment values.Ref
	Prior      AssignmentFields
}

// Kind returns CompRevertAssignment.
func (RevertAssignment) Kind() CompensationKind { return CompRevertAssignment }

func (c RevertAssignment) validate() error {
	return firstErr(need(c.Assignment, values.KindAssignment), c.Prior.validate())
}

// RestoreAssignment undoes change_assignment{delete}: the soft-deleted assignment is restored (ADR-0029).
type RestoreAssignment struct{ Assignment values.Ref }

// Kind returns CompRestoreAssignment.
func (RestoreAssignment) Kind() CompensationKind { return CompRestoreAssignment }

func (c RestoreAssignment) validate() error { return need(c.Assignment, values.KindAssignment) }

// DeleteAssignment undoes change_assignment{restore}: the restored assignment is soft-deleted again.
type DeleteAssignment struct{ Assignment values.Ref }

// Kind returns CompDeleteAssignment.
func (DeleteAssignment) Kind() CompensationKind { return CompDeleteAssignment }

func (c DeleteAssignment) validate() error { return need(c.Assignment, values.KindAssignment) }

// DoneMark is the prior state of one assignment mark_assignments_done changed: WasDone is the state before the call,
// Version the version the call saved, which the Undo expects (homeworkflow.DonePrior).
type DoneMark struct {
	Assignment values.Ref
	WasDone    bool
	Version    int64
}

// RestoreDone undoes mark_assignments_done: one compensation for the whole list (one journal entry, ADR-0033 §9).
// Only the assignments the use case changed are in it, each with the version it saved; the use case puts each back
// while it is at that version and reports a conflict for the others (TASK-314). The marks are held in an array so the
// compensation stays comparable like the others.
type RestoreDone struct {
	n     int
	marks [values.MaxMarkAssignments]DoneMark
}

// NewRestoreDone builds the compensation from the prior states the use case returned: 1..MaxMarkAssignments
// distinct assignments, each with the version it saved.
func NewRestoreDone(marks []DoneMark) (RestoreDone, error) {
	if len(marks) == 0 || len(marks) > values.MaxMarkAssignments {
		return RestoreDone{}, fmt.Errorf("%w: 1..%d marks", ErrInvalidCompensation, values.MaxMarkAssignments)
	}
	c := RestoreDone{n: len(marks)}
	copy(c.marks[:], marks)
	if err := c.validate(); err != nil {
		return RestoreDone{}, err
	}
	return c, nil
}

// Kind returns CompRestoreDone.
func (RestoreDone) Kind() CompensationKind { return CompRestoreDone }

// Marks returns the prior states, in the order of the use case.
func (c RestoreDone) Marks() []DoneMark {
	if c.n < 0 || c.n > len(c.marks) {
		return nil
	}
	return append([]DoneMark(nil), c.marks[:c.n]...)
}

// ExpectedVersion is the highest version of the marks: the action's TargetVersion, as a list has no single target.
func (c RestoreDone) ExpectedVersion() int64 {
	var v int64
	for _, m := range c.Marks() {
		v = max(v, m.Version)
	}
	return v
}

func (c RestoreDone) validate() error {
	if c.n < 1 || c.n > len(c.marks) {
		return fmt.Errorf("%w: 1..%d marks", ErrInvalidCompensation, values.MaxMarkAssignments)
	}
	seen := values.RefSet{}
	for i, m := range c.marks {
		if i >= c.n {
			if m != (DoneMark{}) {
				return fmt.Errorf("%w: a mark past the list", ErrInvalidCompensation)
			}
			continue
		}
		if err := need(m.Assignment, values.KindAssignment); err != nil {
			return err
		}
		if m.Version < 1 || seen.Has(m.Assignment) {
			return fmt.Errorf("%w: distinct marks, each with the version it saved", ErrInvalidCompensation)
		}
		seen.Add(m.Assignment)
	}
	return nil
}

// RemoveNotePart undoes note{add_text|add_upload} into an existing note: the part the action added (a material) is
// taken out again.
type RemoveNotePart struct{ Note, Part values.Ref }

// Kind returns CompRemoveNotePart.
func (RemoveNotePart) Kind() CompensationKind { return CompRemoveNotePart }

func (c RemoveNotePart) validate() error {
	return firstErr(need(c.Note, values.KindNote), need(c.Part, values.KindMaterial))
}

// RemoveNote undoes note{add_text|add_upload} that made a new note: it is removed (soft).
type RemoveNote struct{ Note values.Ref }

// Kind returns CompRemoveNote.
func (RemoveNote) Kind() CompensationKind { return CompRemoveNote }

func (c RemoveNote) validate() error { return need(c.Note, values.KindNote) }

// NoteAnchorKind is what a note was attached to (library/note anchor kinds).
type NoteAnchorKind string

// Anchor kinds.
const (
	AnchorRecording NoteAnchorKind = "recording"
	AnchorLesson    NoteAnchorKind = "lesson"
	AnchorDay       NoteAnchorKind = "day"
	AnchorUndated   NoteAnchorKind = "undated"
)

// NoteAnchor is the anchor of a note before note{move}: a recording or a lesson with its date, a day, or no date.
type NoteAnchor struct {
	Kind   NoteAnchorKind
	Target values.Ref // the recording or the lesson; zero for a day or undated
	Date   values.Date
}

func (a NoteAnchor) validate() error {
	var kind values.IDKind
	switch a.Kind {
	case AnchorRecording:
		kind = values.KindRecording
	case AnchorLesson:
		kind = values.KindLesson
	case AnchorDay, AnchorUndated:
	default:
		return fmt.Errorf("%w: unknown anchor kind", ErrInvalidCompensation)
	}
	if kind != "" {
		if err := need(a.Target, kind); err != nil {
			return err
		}
	} else if !a.Target.IsZero() {
		return fmt.Errorf("%w: a %s anchor has no id", ErrInvalidCompensation, a.Kind)
	}
	if (a.Kind == AnchorUndated) != a.Date.IsZero() {
		return fmt.Errorf("%w: only an undated anchor has no date", ErrInvalidCompensation)
	}
	return nil
}

// ReanchorNote undoes note{move}: the note goes back to Prior, the anchor read before the move (Intent.Prior).
type ReanchorNote struct {
	Note  values.Ref
	Prior NoteAnchor
}

// Kind returns CompReanchorNote.
func (ReanchorNote) Kind() CompensationKind { return CompReanchorNote }

func (c ReanchorNote) validate() error {
	return firstErr(need(c.Note, values.KindNote), c.Prior.validate())
}

// RetitleNote undoes note{rename}: the title goes back to OldTitle, read before the rename (Intent.Prior).
type RetitleNote struct {
	Note     values.Ref
	OldTitle string
}

// Kind returns CompRetitleNote.
func (RetitleNote) Kind() CompensationKind { return CompRetitleNote }

func (c RetitleNote) validate() error {
	if err := need(c.Note, values.KindNote); err != nil {
		return err
	}
	if strings.TrimSpace(c.OldTitle) == "" || !oneLine(c.OldTitle, maxNoteTitleRunes) {
		return fmt.Errorf("%w: old title is not one line of text", ErrInvalidCompensation)
	}
	return nil
}

// RestoreNote undoes note{remove}: the soft-removed note is restored.
type RestoreNote struct{ Note values.Ref }

// Kind returns CompRestoreNote.
func (RestoreNote) Kind() CompensationKind { return CompRestoreNote }

func (c RestoreNote) validate() error { return need(c.Note, values.KindNote) }

// RestoreSetting undoes set_setting: the setting goes back to OldValue, read before the change (Intent.Prior). The
// action's TargetVersion is the version of the settings.
type RestoreSetting struct {
	Setting  values.Setting
	OldValue string
}

// Kind returns CompRestoreSetting.
func (RestoreSetting) Kind() CompensationKind { return CompRestoreSetting }

func (c RestoreSetting) validate() error {
	if !values.ValidSettingValue(c.Setting, c.OldValue) {
		return fmt.Errorf("%w: old value is not a value of the setting", ErrInvalidCompensation)
	}
	return nil
}

// StopRecording undoes recording{start}: the recording is stopped and its audio kept (ADR-0033 §10).
type StopRecording struct{ Recording values.Ref }

// Kind returns CompStopRecording.
func (StopRecording) Kind() CompensationKind { return CompStopRecording }

func (c StopRecording) validate() error { return need(c.Recording, values.KindRecording) }

// targetOfWave10 is targetOf for the wave-10 kinds: zero for a list of marks and for a setting (no ID).
func targetOfWave10(c Compensation) values.Ref {
	switch v := c.(type) {
	case RevertAssignment:
		return v.Assignment
	case RestoreAssignment:
		return v.Assignment
	case DeleteAssignment:
		return v.Assignment
	case RemoveNotePart:
		return v.Note
	case RemoveNote:
		return v.Note
	case ReanchorNote:
		return v.Note
	case RetitleNote:
		return v.Note
	case RestoreNote:
		return v.Note
	case StopRecording:
		return v.Recording
	}
	return values.Ref{}
}

// oneLine reports whether s is valid UTF-8 of at most maxRunes characters without control characters.
func oneLine(s string, maxRunes int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= maxRunes && !strings.ContainsFunc(s, unicode.IsControl)
}

// textBlock is oneLine that allows line breaks and tabs.
func textBlock(s string, maxRunes int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= maxRunes &&
		!strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' })
}
