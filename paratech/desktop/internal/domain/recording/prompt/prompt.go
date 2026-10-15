package prompt

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// SnoozeDelay is how long a snoozed prompt stays hidden before it is offered again (fixed, no setting in MVP).
const SnoozeDelay = 5 * time.Minute

// maxIDBytes bounds a prompt ID; IDs are short in-memory handles the popup sends back.
const maxIDBytes = 64

// ID identifies one prompt; a click on an old popup carries an old ID and never acts on a newer prompt.
type ID string

// String returns the identifier as a plain string.
func (id ID) String() string { return string(id) }

// Validate reports whether the identifier is 1..64 bytes of ASCII letters, digits, '-' and '_'.
func (id ID) Validate() error {
	if id == "" || len(id) > maxIDBytes {
		return fmt.Errorf("%w: prompt id of %d bytes, want 1..%d", ErrInvalidPrompt, len(id), maxIDBytes)
	}
	for _, r := range id {
		ok := r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !ok {
			return fmt.Errorf("%w: prompt id has a character other than a letter, digit, '-' or '_'", ErrInvalidPrompt)
		}
	}
	return nil
}

// Kind tells a lesson prompt from an ad-hoc one.
type Kind string

// Prompt kinds.
const (
	KindLesson Kind = "lesson" // raised by the schedule when a lesson starts
	KindAdHoc  Kind = "adhoc"  // asked for by the user, without a lesson
)

// Lesson is the lesson a prompt offers to record, copied from the schedule's announcement.
type Lesson struct {
	ID       shared.LessonID
	Title    string // imported text: untrusted, shown as plain text only
	Start    time.Time
	End      time.Time
	TimeZone string // IANA name of the source record; the times are shown in it
}

// Prompt is one offer to record a class (PRD section 6.1). Fields change only through the methods below.
type Prompt struct {
	id           ID
	kind         Kind
	lesson       Lesson // zero for an ad-hoc prompt
	state        State
	problem      Problem
	offeredAt    time.Time
	snoozedUntil time.Time // set while SNOOZED
}

// NewLesson offers to record a lesson that has not ended at now.
func NewLesson(id ID, l Lesson, now time.Time) (*Prompt, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	if err := l.ID.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPrompt, err)
	}
	switch {
	case now.IsZero():
		return nil, fmt.Errorf("%w: no offer time", ErrInvalidPrompt)
	case l.Start.IsZero() || !l.End.After(l.Start):
		return nil, fmt.Errorf("%w: lesson times %v..%v", ErrInvalidPrompt, l.Start, l.End)
	case !now.Before(l.End):
		return nil, fmt.Errorf("%w: the lesson ended at %v", ErrInvalidPrompt, l.End)
	}
	return &Prompt{id: id, kind: KindLesson, lesson: l, state: StateOffered, offeredAt: now}, nil
}

// NewAdHoc offers to record without a lesson. It cannot be snoozed and does not expire.
func NewAdHoc(id ID, now time.Time) (*Prompt, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no offer time", ErrInvalidPrompt)
	}
	return &Prompt{id: id, kind: KindAdHoc, state: StateOffered, offeredAt: now}, nil
}

// ID returns the prompt identifier.
func (p *Prompt) ID() ID { return p.id }

// Kind returns whether this is a lesson or an ad-hoc prompt.
func (p *Prompt) Kind() Kind { return p.kind }

// Lesson returns the offered lesson; zero for an ad-hoc prompt.
func (p *Prompt) Lesson() Lesson { return p.lesson }

// State returns the current state.
func (p *Prompt) State() State { return p.state }

// Problem returns why the last start failed; ProblemNone otherwise.
func (p *Prompt) Problem() Problem { return p.problem }

// OfferedAt returns when the prompt was created.
func (p *Prompt) OfferedAt() time.Time { return p.offeredAt }

// SnoozedUntil returns when a snoozed prompt is due again; zero unless SNOOZED.
func (p *Prompt) SnoozedUntil() time.Time { return p.snoozedUntil }

// BeginStart moves an offered prompt to STARTING. A second start while one runs is refused.
func (p *Prompt) BeginStart() error {
	if err := p.move(StateStarting); err != nil {
		return err
	}
	p.problem = ProblemNone
	return nil
}

// Started records that the recording started; the prompt is over.
func (p *Prompt) Started() error { return p.move(StateStarted) }

// StartFailed puts the prompt back on offer with the reason, so the user can retry or skip.
func (p *Prompt) StartFailed(problem Problem) error {
	if !problem.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidProblem, problem)
	}
	if p.state != StateStarting { // SNOOZED -> OFFERED is a re-show, not a failed start
		return p.refuse(StateOffered)
	}
	p.state, p.problem = StateOffered, problem
	return nil
}

// CanSnooze reports whether Snooze would succeed at now: an offered lesson prompt whose lesson still runs after
// SnoozeDelay.
func (p *Prompt) CanSnooze(now time.Time) bool {
	return p.kind == KindLesson && p.state == StateOffered && now.Add(SnoozeDelay).Before(p.lesson.End)
}

// Snooze hides the prompt until SnoozeDelay after now.
func (p *Prompt) Snooze(now time.Time) error {
	if !CanTransition(p.state, StateSnoozed) {
		return p.refuse(StateSnoozed)
	}
	if !p.CanSnooze(now) {
		return ErrSnoozeNotAllowed
	}
	p.state, p.problem, p.snoozedUntil = StateSnoozed, ProblemNone, now.Add(SnoozeDelay)
	return nil
}

// ReshowDue reports whether a snoozed prompt is to be offered again at now: its delay has passed and the lesson has
// not ended.
func (p *Prompt) ReshowDue(now time.Time) bool {
	return p.state == StateSnoozed && !now.Before(p.snoozedUntil) && now.Before(p.lesson.End)
}

// Reshow offers a snoozed prompt again once ReshowDue.
func (p *Prompt) Reshow(now time.Time) error {
	if p.state != StateSnoozed { // STARTING -> OFFERED is a failed start, not a re-show
		return p.refuse(StateOffered)
	}
	if !p.ReshowDue(now) {
		return fmt.Errorf("%w: show again at %v, lesson ends at %v", ErrNotDue, p.snoozedUntil, p.lesson.End)
	}
	p.state, p.snoozedUntil = StateOffered, time.Time{}
	return nil
}

// Withdraw ends a snoozed prompt because prompts were turned off meanwhile: it is not shown again.
func (p *Prompt) Withdraw() error {
	if err := p.move(StateWithdrawn); err != nil {
		return err
	}
	p.snoozedUntil = time.Time{}
	return nil
}

// Skip ends the prompt at the user's request; its lesson is not offered again.
func (p *Prompt) Skip() error {
	if err := p.move(StateSkipped); err != nil {
		return err
	}
	p.snoozedUntil = time.Time{}
	return nil
}

// ExpiryDue reports whether an offered or snoozed lesson prompt is to end at now because its lesson ended. A start in
// progress is not cut off; an ad-hoc prompt never expires.
func (p *Prompt) ExpiryDue(now time.Time) bool {
	return p.kind == KindLesson && (p.state == StateOffered || p.state == StateSnoozed) && !now.Before(p.lesson.End)
}

// Expire ends the prompt once ExpiryDue.
func (p *Prompt) Expire(now time.Time) error {
	if !CanTransition(p.state, StateExpired) {
		return p.refuse(StateExpired)
	}
	if !p.ExpiryDue(now) {
		return fmt.Errorf("%w: the lesson ends at %v", ErrNotDue, p.lesson.End)
	}
	p.state, p.snoozedUntil = StateExpired, time.Time{}
	return nil
}

// move applies one transition of the table.
func (p *Prompt) move(to State) error {
	if !CanTransition(p.state, to) {
		return p.refuse(to)
	}
	p.state = to
	return nil
}

func (p *Prompt) refuse(to State) error {
	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, p.state, to)
}
