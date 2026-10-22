package assignment

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// noSource is the source index of an assignment that was not extracted from a transcript.
const noSource = -1

// Origin identifies the item of an extraction answer an assignment was made from: the transcript, or the material of
// the library (TASK-122), and the position of the item in the answer (from 0). Reprocessing the source finds the
// assignment again by its origin.
type Origin struct {
	Transcript shared.TranscriptID // exactly one of Transcript and Material
	Material   shared.MaterialID
	Index      int
}

// Validate checks that exactly one source id is given and valid and that the index is not negative.
func (o Origin) Validate() error {
	switch {
	case o.Transcript != "" && o.Material != "":
		return fmt.Errorf("a transcript and a material")
	case o.Material != "":
		if err := o.Material.Validate(); err != nil {
			return err
		}
	default:
		if err := o.Transcript.Validate(); err != nil {
			return err
		}
	}
	if o.Index < 0 {
		return fmt.Errorf("negative source index %d", o.Index)
	}
	return nil
}

// Assignment is a homework item (PRD section 10). Fields change only through the methods below.
type Assignment struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.AssignmentID

	lesson        shared.LessonID     // optional, by ID only (ARCH-13)
	transcript    shared.TranscriptID // empty after the transcript was deleted (ON DELETE SET NULL)
	material      shared.MaterialID   // the material it was extracted from (TASK-122); empty for a transcript
	source        int                 // position in the extraction answer, noSource if not extracted
	promptVersion int                 // prompts version of the extraction that last set the details, 0 if none

	details    Details
	userEdited bool

	hints      values.LessonNames // subject and teacher the extraction found in the transcript (model output)
	correction values.LessonNames // subject and teacher the user entered; empty fields correct nothing

	createdAt time.Time
	updatedAt time.Time
}

// Spec is what the creator of an extracted assignment provides to NewExtracted.
type Spec struct {
	ID            shared.AssignmentID
	Lesson        shared.LessonID // optional
	Origin        Origin
	PromptVersion int // the prompts version of the extraction, at least 1
	Details       Details
	Hints         values.LessonNames // optional
}

// NewExtracted creates an assignment from one item of an extraction answer, never stored (Version() == 0).
func NewExtracted(spec Spec, now time.Time) (*Assignment, error) {
	if err := spec.Origin.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAssignment, err)
	}
	a := &Assignment{
		id: spec.ID, lesson: spec.Lesson, transcript: spec.Origin.Transcript, material: spec.Origin.Material,
		source: spec.Origin.Index,
		promptVersion: spec.PromptVersion, details: spec.Details, hints: spec.Hints, createdAt: now, updatedAt: now,
	}
	if err := a.validate(); err != nil {
		return nil, err
	}
	return a, nil
}

// State is the stored form of an assignment, for Restore and the repositories.
type State struct {
	Version       int64
	ID            shared.AssignmentID
	Lesson        shared.LessonID     // empty: none
	Transcript    shared.TranscriptID // empty: none, or the transcript was deleted
	Material      shared.MaterialID   // empty: none, or the material was deleted
	SourceIndex   int                 // -1: not extracted
	PromptVersion int                 // 0: none
	Title         string
	Description   string
	Due           time.Time // zero: no deadline
	UserEdited    bool
	HintSubject   string // "": none
	HintTeacher   string
	UserSubject   string // "": not corrected
	UserTeacher   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Restore rebuilds a stored assignment without side effects; it only checks that the state is consistent.
func Restore(s State) (*Assignment, error) {
	if s.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidAssignment, s.Version)
	}
	d, err := NewDetails(s.Title, s.Description, s.Due)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAssignment, err)
	}
	if s.Transcript != "" {
		if err := s.Transcript.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidAssignment, err)
		}
	}
	if s.Material != "" {
		if err := s.Material.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidAssignment, err)
		}
		if s.Transcript != "" {
			return nil, fmt.Errorf("%w: a transcript and a material", ErrInvalidAssignment)
		}
	}
	a := &Assignment{
		version: s.Version, id: s.ID, lesson: s.Lesson, transcript: s.Transcript, material: s.Material,
		source: s.SourceIndex,
		promptVersion: s.PromptVersion, details: d, userEdited: s.UserEdited,
		hints:      values.NewLessonNames(s.HintSubject, s.HintTeacher),
		correction: values.NewLessonNames(s.UserSubject, s.UserTeacher),
		createdAt:  s.CreatedAt, updatedAt: s.UpdatedAt,
	}
	if err := a.validate(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Assignment) validate() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidAssignment, fmt.Sprintf(format, args...))
	}
	if err := a.id.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAssignment, err)
	}
	if a.lesson != "" {
		if err := a.lesson.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidAssignment, err)
		}
	}
	switch {
	case a.details.IsZero():
		return bad("no details")
	case a.source < noSource:
		return bad("source index %d", a.source)
	case a.source >= 0 && a.promptVersion < 1:
		return bad("extracted without a prompt version")
	case a.promptVersion < 0:
		return bad("prompt version %d", a.promptVersion)
	case a.createdAt.IsZero() || a.updatedAt.IsZero():
		return bad("missing time")
	}
	return nil
}

// Reextract applies the details of the same item of a new extraction answer (reprocessing, PRD section 10).
// An assignment the user edited is kept as it is: the result is false and nothing changes. Otherwise the details and
// the prompt version are replaced when they differ, and changed tells whether the assignment must be saved.
func (a *Assignment) Reextract(d Details, promptVersion int, now time.Time) (changed bool, err error) {
	switch {
	case a.source == noSource:
		return false, ErrNotExtracted
	case d.IsZero():
		return false, fmt.Errorf("%w: no details", ErrInvalidDetails)
	case promptVersion < 1:
		return false, fmt.Errorf("%w: prompt version %d", ErrInvalidAssignment, promptVersion)
	case a.userEdited:
		return false, nil
	case a.details.Equal(d) && a.promptVersion == promptVersion:
		return false, nil
	}
	a.details, a.promptVersion, a.updatedAt = d, promptVersion, now
	return true, nil
}

// SetHints stores the subject and teacher the extraction of the same item found in the transcript. They are not user
// data, so a new extraction replaces them even when the user edited the details; they never replace the schedule or
// the user's correction (values.NewLessonLabel). changed tells whether the assignment must be saved.
func (a *Assignment) SetHints(h values.LessonNames, now time.Time) (changed bool, err error) {
	if a.source == noSource {
		return false, ErrNotExtracted
	}
	if a.hints == h {
		return false, nil
	}
	a.hints, a.updatedAt = h, now
	return true, nil
}

// Correct stores the subject and teacher the user entered; an empty field removes the correction of that field, so
// the schedule or the hint shows again. changed tells whether the assignment must be saved.
func (a *Assignment) Correct(c values.LessonNames, now time.Time) (changed bool) {
	if a.correction == c {
		return false
	}
	a.correction, a.updatedAt = c, now
	return true
}

// Superseded reports whether a new extraction answer of the same transcript with items items leaves the assignment
// behind, so that reprocessing may withdraw (delete) it: it was extracted at a position the answer no longer has and
// the user never edited it. Matching is by position only (see the package doc).
func (a *Assignment) Superseded(items int) bool {
	return a.source != noSource && a.source >= items && !a.userEdited
}

// Edit stores the details the user entered (PRD section 10: review and edit locally, AC-13). From then on the
// assignment is user-edited and reprocessing never overwrites it. Saving unchanged details also counts: the user has
// confirmed them.
func (a *Assignment) Edit(d Details, now time.Time) error {
	if d.IsZero() {
		return fmt.Errorf("%w: no details", ErrInvalidDetails)
	}
	a.details, a.userEdited, a.updatedAt = d, true, now
	return nil
}

// ID returns the assignment identifier.
func (a *Assignment) ID() shared.AssignmentID { return a.id }

// LessonID returns the lesson the assignment was given in, empty if unknown.
func (a *Assignment) LessonID() shared.LessonID { return a.lesson }

// TranscriptID returns the transcript the assignment was extracted from, empty if none.
func (a *Assignment) TranscriptID() shared.TranscriptID { return a.transcript }

// MaterialID returns the material the assignment was extracted from (TASK-122), empty if none.
func (a *Assignment) MaterialID() shared.MaterialID { return a.material }

// Origin returns the item of the extraction answer the assignment was made from; ok is false if it was not extracted
// or its source was deleted.
func (a *Assignment) Origin() (o Origin, ok bool) {
	if a.source == noSource || (a.transcript == "" && a.material == "") {
		return Origin{}, false
	}
	return Origin{Transcript: a.transcript, Material: a.material, Index: a.source}, true
}

// SourceIndex returns the position in the extraction answer, -1 if the assignment was not extracted.
func (a *Assignment) SourceIndex() int { return a.source }

// PromptVersion returns the prompts version of the extraction that last set the details, 0 if none.
func (a *Assignment) PromptVersion() int { return a.promptVersion }

// Details returns the title, description and deadline.
func (a *Assignment) Details() Details { return a.details }

// UserEdited reports whether the user edited the assignment; reprocessing then leaves it alone.
func (a *Assignment) UserEdited() bool { return a.userEdited }

// Hints returns the subject and teacher the extraction found in the transcript; model output, shown marked.
func (a *Assignment) Hints() values.LessonNames { return a.hints }

// Correction returns the subject and teacher the user entered.
func (a *Assignment) Correction() values.LessonNames { return a.correction }

// CreatedAt returns the creation time.
func (a *Assignment) CreatedAt() time.Time { return a.createdAt }

// UpdatedAt returns the time of the last change.
func (a *Assignment) UpdatedAt() time.Time { return a.updatedAt }

// State returns the stored form of the assignment, for the repositories.
func (a *Assignment) State() State {
	return State{
		Version: a.version, ID: a.id, Lesson: a.lesson, Transcript: a.transcript, Material: a.material, SourceIndex: a.source,
		PromptVersion: a.promptVersion, Title: a.details.title, Description: a.details.description, Due: a.details.due,
		UserEdited: a.userEdited, HintSubject: a.hints.Subject(), HintTeacher: a.hints.Teacher(),
		UserSubject: a.correction.Subject(), UserTeacher: a.correction.Teacher(), CreatedAt: a.createdAt, UpdatedAt: a.updatedAt,
	}
}

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (a *Assignment) Version() int64 { return a.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (a *Assignment) Persisted(version int64) { a.version = version }
