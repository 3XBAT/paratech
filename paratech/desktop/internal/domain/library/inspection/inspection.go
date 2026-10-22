package inspection

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// MaxAttempts bounds the reader calls for one text: each refused answer is charged, so a text the reader keeps
// refusing is not asked about again.
const MaxAttempts = 3

// Status is the outcome of the last reader call for the text.
type Status string

// Statuses.
const (
	StatusDone    Status = "done"    // a checked answer is kept
	StatusRefused Status = "refused" // the answer broke the schema (on the Hub or here)
)

// Statuses returns every status, for the schema checks.
func Statuses() []Status { return []Status{StatusDone, StatusRefused} }

// Inspection is the outcome of the reader for the text of one material. Fields change only through the methods.
type Inspection struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)

	material shared.MaterialID
	text     material.Digest // the digest of the text the outcome is for (material.Text.Digest)
	status   Status
	attempt  int    // the attempt of the outcome, 1..MaxAttempts
	answer   Answer // zero unless done

	createdAt time.Time
	updatedAt time.Time
}

// Plan is what to do for a text: use the kept answer, or ask with the attempt.
type Plan struct {
	Cached  bool
	Answer  Answer // the kept answer when Cached
	Attempt int    // the attempt to ask with when not Cached
}

// First is the plan for a material never inspected.
func First() Plan { return Plan{Attempt: 1} }

// Next tells what to do for text d: the kept answer when it is done for d; a refused d is asked again with the next
// attempt, up to MaxAttempts (then ErrRefused); another text starts at attempt 1.
func (i *Inspection) Next(d material.Digest) (Plan, error) {
	switch {
	case i.text != d:
		return First(), nil
	case i.status == StatusDone:
		return Plan{Cached: true, Answer: i.answer}, nil
	case i.attempt >= MaxAttempts:
		return Plan{}, ErrRefused
	}
	return Plan{Attempt: i.attempt + 1}, nil
}

// New records the first outcome for a material: answer is nil for a refused one. Error: ErrInvalidInspection.
func New(m shared.MaterialID, d material.Digest, attempt int, answer *Answer, now time.Time) (*Inspection, error) {
	i := &Inspection{material: m, createdAt: now}
	if err := i.Record(d, attempt, answer, now); err != nil {
		return nil, err
	}
	return i, nil
}

// Record keeps the outcome of an attempt for text d: the answer, or a refusal when answer is nil. For the same text
// an attempt before the kept one is refused (an older call ended late). Error: ErrInvalidInspection.
func (i *Inspection) Record(d material.Digest, attempt int, answer *Answer, now time.Time) error {
	if d == i.text && attempt < i.attempt {
		return fmt.Errorf("%w: attempt %d after %d", ErrInvalidInspection, attempt, i.attempt)
	}
	next := *i
	next.text, next.attempt, next.updatedAt = d, attempt, now
	next.status, next.answer = StatusRefused, Answer{}
	if answer != nil {
		next.status, next.answer = StatusDone, *answer
	}
	if err := next.validate(); err != nil {
		return err
	}
	*i = next
	return nil
}

// State is the stored form, for Restore and the repositories.
type State struct {
	Version   int64
	Material  shared.MaterialID
	Text      material.Digest
	Status    Status
	Attempt   int
	Answer    Answer
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Restore rebuilds a stored inspection without side effects; it only checks that the state is consistent.
func Restore(st State) (*Inspection, error) {
	if st.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidInspection, st.Version)
	}
	i := &Inspection{
		version: st.Version, material: st.Material, text: st.Text, status: st.Status, attempt: st.Attempt,
		answer: st.Answer, createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
	if err := i.validate(); err != nil {
		return nil, err
	}
	return i, nil
}

func (i *Inspection) validate() error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrInvalidInspection, why) }
	if i.material.Validate() != nil {
		return bad("material id")
	}
	if i.text.Validate() != nil {
		return bad("text digest")
	}
	switch {
	case i.attempt < 1 || i.attempt > MaxAttempts:
		return bad("attempt")
	case i.createdAt.IsZero() || i.updatedAt.IsZero():
		return bad("missing time")
	case i.status == StatusRefused && i.answer != (Answer{}):
		return bad("a refused inspection keeps no answer")
	case i.status != StatusRefused && i.status != StatusDone:
		return bad("status")
	case i.status == StatusDone && !i.answer.DocKind.Valid():
		return bad("document kind")
	case i.answer.Subject != "" && i.answer.Subject.Validate() != nil:
		return bad("subject id")
	}
	if d := i.answer.LessonDate; !d.IsZero() && (d.Hour() != 0 || d.Minute() != 0 || d.Second() != 0 || d.Nanosecond() != 0 || d.Location() != time.UTC) {
		return bad("lesson date is not a day")
	}
	return nil
}

// Material returns the material.
func (i *Inspection) Material() shared.MaterialID { return i.material }

// Text returns the digest of the text the outcome is for.
func (i *Inspection) Text() material.Digest { return i.text }

// Status returns the outcome of the last call.
func (i *Inspection) Status() Status { return i.status }

// Attempt returns the attempt of the outcome.
func (i *Inspection) Attempt() int { return i.attempt }

// Answer returns the kept answer; zero unless done.
func (i *Inspection) Answer() Answer { return i.answer }

// State returns the stored form.
func (i *Inspection) State() State {
	return State{
		Version: i.version, Material: i.material, Text: i.text, Status: i.status, Attempt: i.attempt,
		Answer: i.answer, CreatedAt: i.createdAt, UpdatedAt: i.updatedAt,
	}
}

// Version returns the version this instance was last loaded or saved with: 0 for one never stored.
func (i *Inspection) Version() int64 { return i.version }

// Persisted records the version the store assigned after a successful Save; called by the repository adapter only.
func (i *Inspection) Persisted(version int64) { i.version = version }
