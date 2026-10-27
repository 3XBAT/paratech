package note

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// Limits of the aggregate.
const (
	// MaxTitleRunes bounds the display title.
	MaxTitleRunes = 120

	// MaxParts bounds the parts of one note.
	MaxParts = 200
)

// Status is whether the note is shown or removed (soft delete).
type Status string

// Statuses of a note.
const (
	StatusActive  Status = "active"
	StatusRemoved Status = "removed"
)

// Statuses returns every status, for the schema checks.
func Statuses() []Status { return []Status{StatusActive, StatusRemoved} }

// transitions is the status machine (ARCH-19).
var transitions = map[Status][]Status{
	StatusActive:  {StatusRemoved},
	StatusRemoved: {StatusActive},
}

// Note is the student's own notes of one lecture. Fields change only through the methods below.
type Note struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.NoteID

	subject shared.SubjectID
	anchor  Anchor
	title   string              // display only
	parts   []shared.MaterialID // in the student's order

	status    Status
	createdAt time.Time
	updatedAt time.Time
}

// Spec is what the creator of a note provides to New.
type Spec struct {
	ID      shared.NoteID
	Subject shared.SubjectID
	Anchor  Anchor
	Title   string // the student's text or a file name; cleaned and cut for display
	Parts   []shared.MaterialID
}

// New creates an active note, never stored (Version() == 0). Errors: ErrInvalidNote, ErrInvalidAnchor,
// ErrEmptyTitle, ErrNoParts, ErrTooManyParts, ErrDuplicatePart.
func New(spec Spec, now time.Time) (*Note, error) {
	n := &Note{
		id: spec.ID, subject: spec.Subject, anchor: spec.Anchor, title: cleanTitle(spec.Title),
		parts: slices.Clone(spec.Parts), status: StatusActive, createdAt: now, updatedAt: now,
	}
	if err := n.validate(); err != nil {
		return nil, err
	}
	return n, nil
}

// State is the stored form of a note, for Restore and the repositories.
type State struct {
	Version   int64
	ID        shared.NoteID
	Subject   shared.SubjectID
	Anchor    Anchor
	Title     string
	Parts     []shared.MaterialID
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Restore rebuilds a stored note without side effects; it only checks that the state is consistent.
func Restore(st State) (*Note, error) {
	if st.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidNote, st.Version)
	}
	if cleanTitle(st.Title) != st.Title {
		return nil, fmt.Errorf("%w: stored title", ErrInvalidNote)
	}
	n := &Note{
		version: st.Version, id: st.ID, subject: st.Subject, anchor: st.Anchor, title: st.Title,
		parts: slices.Clone(st.Parts), status: st.Status, createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
	if err := n.validate(); err != nil {
		return nil, err
	}
	return n, nil
}

// cleanTitle makes a title one line of at most MaxTitleRunes characters.
func cleanTitle(s string) string {
	s = values.CleanText(s)
	if utf8.RuneCountInString(s) > MaxTitleRunes {
		s = strings.TrimSpace(string([]rune(s)[:MaxTitleRunes]))
	}
	return s
}

func (n *Note) validate() error {
	if err := n.id.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidNote, err)
	}
	if err := n.subject.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidNote, err)
	}
	if err := n.anchor.validate(); err != nil {
		return err
	}
	switch {
	case n.title == "":
		return ErrEmptyTitle
	case !slices.Contains(Statuses(), n.status):
		return fmt.Errorf("%w: status %q", ErrInvalidNote, n.status)
	case n.createdAt.IsZero() || n.updatedAt.IsZero():
		return fmt.Errorf("%w: missing time", ErrInvalidNote)
	}
	return checkParts(n.parts)
}

// checkParts checks a list of parts: 1…MaxParts valid material ids without repeats.
func checkParts(parts []shared.MaterialID) error {
	switch {
	case len(parts) == 0:
		return ErrNoParts
	case len(parts) > MaxParts:
		return fmt.Errorf("%w: %d", ErrTooManyParts, len(parts))
	}
	seen := make(map[shared.MaterialID]bool, len(parts))
	for _, p := range parts {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidNote, err)
		}
		if seen[p] {
			return fmt.Errorf("%w: %s", ErrDuplicatePart, p)
		}
		seen[p] = true
	}
	return nil
}

// AddParts appends the materials that are not parts yet, in the given order; a material that is already a part, or
// repeated in ids, is added once (the same file twice is «уже добавлено»), so nothing new changes nothing. Errors:
// ErrRemoved, ErrNoParts for an empty ids, ErrInvalidNote for a bad id, ErrTooManyParts (nothing is added).
func (n *Note) AddParts(ids []shared.MaterialID, now time.Time) (changed bool, err error) {
	if len(ids) == 0 {
		return false, ErrNoParts
	}
	for _, id := range ids {
		if err := id.Validate(); err != nil {
			return false, fmt.Errorf("%w: %w", ErrInvalidNote, err)
		}
	}
	if n.status != StatusActive {
		return false, ErrRemoved
	}
	parts := slices.Clone(n.parts)
	for _, id := range ids {
		if !slices.Contains(parts, id) {
			parts = append(parts, id)
		}
	}
	if len(parts) == len(n.parts) {
		return false, nil
	}
	if len(parts) > MaxParts {
		return false, fmt.Errorf("%w: %d", ErrTooManyParts, len(parts))
	}
	n.parts, n.updatedAt = parts, now
	return true, nil
}

// ReplacePart puts the material next in the place of the part old (a changed file is a new material); replacing a
// part by itself changes nothing. Errors: ErrRemoved, ErrInvalidNote for a bad id, ErrPartNotFound for an old that is
// not a part, ErrDuplicatePart for a next that is another part.
func (n *Note) ReplacePart(old, next shared.MaterialID, now time.Time) (changed bool, err error) {
	if err := next.Validate(); err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalidNote, err)
	}
	if n.status != StatusActive {
		return false, ErrRemoved
	}
	i := slices.Index(n.parts, old)
	if i < 0 {
		return false, fmt.Errorf("%w: %s", ErrPartNotFound, old)
	}
	if old == next {
		return false, nil
	}
	if slices.Contains(n.parts, next) {
		return false, fmt.Errorf("%w: %s", ErrDuplicatePart, next)
	}
	n.parts[i], n.updatedAt = next, now
	return true, nil
}

// Reorder sets the order of the parts; order holds every part exactly once. The same order changes nothing. Errors:
// ErrRemoved, ErrInvalidOrder.
func (n *Note) Reorder(order []shared.MaterialID, now time.Time) (changed bool, err error) {
	if n.status != StatusActive {
		return false, ErrRemoved
	}
	if len(order) != len(n.parts) {
		return false, fmt.Errorf("%w: %d parts, want %d", ErrInvalidOrder, len(order), len(n.parts))
	}
	seen := make(map[shared.MaterialID]bool, len(order))
	for _, id := range order {
		if seen[id] || !slices.Contains(n.parts, id) {
			return false, fmt.Errorf("%w: %s", ErrInvalidOrder, id)
		}
		seen[id] = true
	}
	if slices.Equal(order, n.parts) {
		return false, nil
	}
	n.parts, n.updatedAt = slices.Clone(order), now
	return true, nil
}

// Reanchor attaches the note to another anchor (made by NewAnchor or FallBackToDay); the same anchor changes nothing.
// Errors: ErrRemoved, ErrInvalidAnchor for no anchor.
func (n *Note) Reanchor(a Anchor, now time.Time) (changed bool, err error) {
	if err := a.validate(); err != nil {
		return false, err
	}
	if n.status != StatusActive {
		return false, ErrRemoved
	}
	if a == n.anchor {
		return false, nil
	}
	n.anchor, n.updatedAt = a, now
	return true, nil
}

// Retitle sets the display title, cleaned and cut as in New; the same title changes nothing. Errors: ErrRemoved,
// ErrEmptyTitle.
func (n *Note) Retitle(title string, now time.Time) (changed bool, err error) {
	title = cleanTitle(title)
	if title == "" {
		return false, ErrEmptyTitle
	}
	if n.status != StatusActive {
		return false, ErrRemoved
	}
	if title == n.title {
		return false, nil
	}
	n.title, n.updatedAt = title, now
	return true, nil
}

// Remove removes the note softly: it keeps its anchor, title and parts, so Restore brings it back as it was. A
// removed one changes nothing.
func (n *Note) Remove(now time.Time) (changed bool, err error) {
	if n.status == StatusRemoved {
		return false, nil
	}
	return true, n.transition(StatusRemoved, now)
}

// Restore brings a removed note back. An active one changes nothing.
func (n *Note) Restore(now time.Time) (changed bool, err error) {
	if n.status == StatusActive {
		return false, nil
	}
	return true, n.transition(StatusActive, now)
}

func (n *Note) transition(to Status, now time.Time) error {
	if !slices.Contains(transitions[n.status], to) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidTransition, n.status, to)
	}
	n.status, n.updatedAt = to, now
	return nil
}

// ID returns the note id.
func (n *Note) ID() shared.NoteID { return n.id }

// Subject returns the subject of the note.
func (n *Note) Subject() shared.SubjectID { return n.subject }

// Anchor returns what the note is attached to.
func (n *Note) Anchor() Anchor { return n.anchor }

// Title returns the display title.
func (n *Note) Title() string { return n.title }

// Parts returns a copy of the parts in the student's order.
func (n *Note) Parts() []shared.MaterialID { return slices.Clone(n.parts) }

// Status returns the status.
func (n *Note) Status() Status { return n.status }

// Active reports whether the note is shown (not removed).
func (n *Note) Active() bool { return n.status == StatusActive }

// CreatedAt returns the time the note was made.
func (n *Note) CreatedAt() time.Time { return n.createdAt }

// UpdatedAt returns the time of the last change.
func (n *Note) UpdatedAt() time.Time { return n.updatedAt }

// State returns the stored form, for the repositories.
func (n *Note) State() State {
	return State{
		Version: n.version, ID: n.id, Subject: n.subject, Anchor: n.anchor, Title: n.title, Parts: slices.Clone(n.parts),
		Status: n.status, CreatedAt: n.createdAt, UpdatedAt: n.updatedAt,
	}
}

// Version returns the version this instance was last loaded or saved with: 0 for a note never stored.
func (n *Note) Version() int64 { return n.version }

// Persisted records the version the store assigned after a successful Save; called by the repository adapter only.
func (n *Note) Persisted(version int64) { n.version = version }
