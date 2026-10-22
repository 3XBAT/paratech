package action

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/agent/values"
)

// CompensationKind names how a DONE action is undone (ADR-0021).
type CompensationKind string

// Compensation kinds.
const (
	CompRemoveSubject     CompensationKind = "remove_subject"      // removed if still empty
	CompRemoveFolder      CompensationKind = "remove_folder"       // removed if still empty
	CompReturnToInbox     CompensationKind = "return_to_inbox"     // placed upload back to «Входящие», file kept
	CompUnlinkMaterial    CompensationKind = "unlink_material"     // link removed
	CompRestoreLessonInfo CompensationKind = "restore_lesson_info" // old subject and kind
	CompRestoreDue        CompensationKind = "restore_due"         // old due date, or none
	CompCancelJob         CompensationKind = "cancel_job"          // running: cancel; finished: remove the result
)

// compensationOf is the compensation kind of every U tool; a tool not listed has no Undo.
var compensationOf = map[values.Tool]CompensationKind{
	values.ToolCreateSubject:    CompRemoveSubject,
	values.ToolCreateFolder:     CompRemoveFolder,
	values.ToolPlaceUpload:      CompReturnToInbox,
	values.ToolLinkMaterial:     CompUnlinkMaterial,
	values.ToolSetLessonInfo:    CompRestoreLessonInfo,
	values.ToolEditDue:          CompRestoreDue,
	values.ToolMakeSummary:      CompCancelJob,
	values.ToolMakeLectureText:  CompCancelJob,
	values.ToolFindHomework:     CompCancelJob,
	values.ToolGenerateDraft:    CompCancelJob,
	values.ToolTriageAssignment: CompCancelJob,
	values.ToolWriteWork:        CompCancelJob,
}

// CompensationFor returns the compensation kind of a tool.
func CompensationFor(t values.Tool) (CompensationKind, bool) {
	k, ok := compensationOf[t]
	return k, ok
}

// Compensation is the typed data Undo needs. The set is closed: only the types below implement it.
type Compensation interface {
	Kind() CompensationKind
	validate() error
}

// RemoveSubject undoes create_subject.
type RemoveSubject struct{ Subject values.Ref }

// Kind returns CompRemoveSubject.
func (RemoveSubject) Kind() CompensationKind { return CompRemoveSubject }

func (c RemoveSubject) validate() error { return need(c.Subject, values.KindSubject) }

// RemoveFolder undoes create_folder.
type RemoveFolder struct{ Subject, Folder values.Ref }

// Kind returns CompRemoveFolder.
func (RemoveFolder) Kind() CompensationKind { return CompRemoveFolder }

func (c RemoveFolder) validate() error {
	return firstErr(need(c.Subject, values.KindSubject), need(c.Folder, values.KindFolder))
}

// ReturnToInbox undoes place_upload: the material goes back to «Входящие»; the file is kept. Lesson is the lesson the
// action linked it to (zero: none), unlinked again.
type ReturnToInbox struct{ Material, Lesson values.Ref }

// Kind returns CompReturnToInbox.
func (ReturnToInbox) Kind() CompensationKind { return CompReturnToInbox }

func (c ReturnToInbox) validate() error {
	if !c.Lesson.IsZero() {
		if err := need(c.Lesson, values.KindLesson); err != nil {
			return err
		}
	}
	return need(c.Material, values.KindMaterial)
}

// UnlinkMaterial undoes link_material.
type UnlinkMaterial struct{ Material, Lesson values.Ref }

// Kind returns CompUnlinkMaterial.
func (UnlinkMaterial) Kind() CompensationKind { return CompUnlinkMaterial }

func (c UnlinkMaterial) validate() error {
	return firstErr(need(c.Material, values.KindMaterial), need(c.Lesson, values.KindLesson))
}

// maxOldSubject bounds the stored old subject correction (a local value, never sent to the planner).
const maxOldSubject = 512

// RestoreLessonInfo undoes set_lesson_info with the correction before it (D7): the subject string the student or the
// agent set and the kind code; empty means no correction. The string stays local: Undo writes it back as it was.
type RestoreLessonInfo struct {
	Recording  values.Ref
	OldSubject string
	OldKind    string
}

// Kind returns CompRestoreLessonInfo.
func (RestoreLessonInfo) Kind() CompensationKind { return CompRestoreLessonInfo }

func (c RestoreLessonInfo) validate() error {
	if err := need(c.Recording, values.KindRecording); err != nil {
		return err
	}
	if len(c.OldSubject) > maxOldSubject || !utf8.ValidString(c.OldSubject) || strings.ContainsFunc(c.OldSubject, unicode.IsControl) {
		return fmt.Errorf("%w: old subject is not one line of text", ErrInvalidCompensation)
	}
	spec, _ := values.SpecOf(values.ToolSetLessonInfo)
	for _, a := range spec.Args() {
		if a.Name == "kind" && c.OldKind != "" && !slices.Contains(a.Values, c.OldKind) {
			return fmt.Errorf("%w: old lesson kind outside the enum", ErrInvalidCompensation)
		}
	}
	return nil
}

// RestoreDue undoes edit_due with the old deadline as it was stored (D7: date and time); zero means there was none.
type RestoreDue struct {
	Assignment values.Ref
	OldDue     time.Time
}

// Kind returns CompRestoreDue.
func (RestoreDue) Kind() CompensationKind { return CompRestoreDue }

func (c RestoreDue) validate() error { return need(c.Assignment, values.KindAssignment) }

// CancelJob undoes a paid job: cancelled while it runs, its result removed when finished.
type CancelJob struct{ Job values.Ref }

// Kind returns CompCancelJob.
func (CancelJob) Kind() CompensationKind { return CompCancelJob }

func (c CancelJob) validate() error { return need(c.Job, values.KindJob) }

func need(r values.Ref, kind values.IDKind) error {
	if r.IsZero() || r.Kind() != kind {
		return fmt.Errorf("%w: want a %s id", ErrInvalidCompensation, kind)
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
