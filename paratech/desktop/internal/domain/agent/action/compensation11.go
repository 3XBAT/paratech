package action

import (
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/agent/values"
)

// Compensations of the wave-11 library tools (ADR-0033 §11). The trust rule of compensation10.go holds: what an Undo
// writes back (the prior place, link, folder name, alias) comes from the store, read by the case before the use case
// runs (Intent.Prior, priorPinned) or returned by it, never from the arguments of the call.
const (
	CompPlaceBack       CompensationKind = "place_back"       // move_to_inbox: back to the prior subject and folder
	CompRelinkMaterial  CompensationKind = "relink_material"  // unlink_material: the link to the prior lesson again
	CompRestoreMaterial CompensationKind = "restore_material" // remove_material: RestoreMaterial (soft delete)
	CompRemoveAlias     CompensationKind = "remove_alias"     // add_alias: the alias it added is removed
	CompRecreateFolder  CompensationKind = "recreate_folder"  // remove_folder: the same ID and name again
	// CompRestoreSubject undoes remove_subject: RestoreSubject of the soft-removed subject. The use case keeps a removed
	// subject for 3 days (TASK-288) and refuses the restore after; an empty subject is removed softly too, so it has the
	// same Undo.
	CompRestoreSubject CompensationKind = "restore_subject"
)

// Bounds of stored prior values of the library (local, never sent to the planner).
const (
	maxPriorNameRunes = 200 // subject.MaxNameRunes: an alias or a folder name
)

// PlaceBack undoes move_to_inbox: the material goes back to Subject and Folder, where it was before (Intent.Prior).
type PlaceBack struct{ Material, Subject, Folder values.Ref }

// Kind returns CompPlaceBack.
func (PlaceBack) Kind() CompensationKind { return CompPlaceBack }

func (c PlaceBack) validate() error {
	return firstErr(need(c.Material, values.KindMaterial), need(c.Subject, values.KindSubject), need(c.Folder, values.KindFolder))
}

// RelinkMaterial undoes unlink_material: the material is linked to Lesson again. The case journals it as the Prior only
// when the stored material was linked to that lesson, so an Undo never adds a link that was not there.
type RelinkMaterial struct{ Material, Lesson values.Ref }

// Kind returns CompRelinkMaterial.
func (RelinkMaterial) Kind() CompensationKind { return CompRelinkMaterial }

func (c RelinkMaterial) validate() error {
	return firstErr(need(c.Material, values.KindMaterial), need(c.Lesson, values.KindLesson))
}

// RestoreMaterial undoes remove_material: the soft-removed material comes back where it was.
type RestoreMaterial struct{ Material values.Ref }

// Kind returns CompRestoreMaterial.
func (RestoreMaterial) Kind() CompensationKind { return CompRestoreMaterial }

func (c RestoreMaterial) validate() error { return need(c.Material, values.KindMaterial) }

// RemoveAlias undoes add_alias: Alias is the suggested subject string the suggestion ID stood for, read from the
// library before the action (Intent.Prior), never the model's text.
type RemoveAlias struct {
	Subject values.Ref
	Alias   string
}

// Kind returns CompRemoveAlias.
func (RemoveAlias) Kind() CompensationKind { return CompRemoveAlias }

func (c RemoveAlias) validate() error {
	if err := need(c.Subject, values.KindSubject); err != nil {
		return err
	}
	return priorName(c.Alias, "alias")
}

// RecreateFolder undoes remove_folder: the own folder comes back with the same ID and Name, read before the removal
// (Intent.Prior). Folders of a subject are flat, so there is no parent. The action's TargetVersion is the subject's.
type RecreateFolder struct {
	Subject, Folder values.Ref
	Name            string
}

// Kind returns CompRecreateFolder.
func (RecreateFolder) Kind() CompensationKind { return CompRecreateFolder }

func (c RecreateFolder) validate() error {
	if err := firstErr(need(c.Subject, values.KindSubject), need(c.Folder, values.KindFolder)); err != nil {
		return err
	}
	return priorName(c.Name, "folder name")
}

// RestoreSubject undoes remove_subject (see CompRestoreSubject).
type RestoreSubject struct{ Subject values.Ref }

// Kind returns CompRestoreSubject.
func (RestoreSubject) Kind() CompensationKind { return CompRestoreSubject }

func (c RestoreSubject) validate() error { return need(c.Subject, values.KindSubject) }

func priorName(s, what string) error {
	if strings.TrimSpace(s) == "" || !oneLine(s, maxPriorNameRunes) {
		return fmt.Errorf("%w: prior %s is not one line of text", ErrInvalidCompensation, what)
	}
	return nil
}

// targetOfWave11 is targetOf for the wave-11 kinds: the material, or the subject for an alias, a folder (as
// RemoveFolder) and a subject.
func targetOfWave11(c Compensation) values.Ref {
	switch v := c.(type) {
	case PlaceBack:
		return v.Material
	case RelinkMaterial:
		return v.Material
	case RestoreMaterial:
		return v.Material
	case RemoveAlias:
		return v.Subject
	case RecreateFolder:
		return v.Subject
	case RestoreSubject:
		return v.Subject
	}
	return values.Ref{}
}
