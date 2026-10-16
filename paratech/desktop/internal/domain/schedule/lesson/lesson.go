package lesson

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Lesson is one schedule entry (PRD section 11). Fields change only through the methods below.
type Lesson struct {
	version   int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id        shared.LessonID
	content   Content
	sourceUID string // "" for a manual lesson

	startNotifiedAt time.Time // zero: the "lesson is starting" prompt was not raised

	// Calendar projection (ADR-0004(e)): contentRevision grows on every change the calendar shows, including deletion;
	// syncedRevision is the revision last written to the calendar, 0 when never written.
	contentRevision int64
	syncedRevision  int64
	syncedAt        time.Time

	deletedAt time.Time // zero: live
	createdAt time.Time
	updatedAt time.Time
}

// Spec is what the creator of a lesson provides to New.
type Spec struct {
	ID        shared.LessonID
	Content   Content
	SourceUID string // identity in the imported file; empty for a manual lesson
}

// New creates a live lesson at content revision 1 that was never written to the calendar.
func New(spec Spec, now time.Time) (*Lesson, error) {
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no creation time", ErrInvalidLesson)
	}
	l := &Lesson{
		id: spec.ID, content: spec.Content, sourceUID: spec.SourceUID,
		contentRevision: 1, createdAt: now, updatedAt: now,
	}
	if err := l.validate(); err != nil {
		return nil, err
	}
	return l, nil
}

// State is the stored form of a lesson, for Restore.
type State struct {
	Version                int64
	ID                     shared.LessonID
	Content                Content
	SourceUID              string
	StartNotifiedAt        time.Time
	ContentRevision        int64
	CalendarSyncedRevision int64 // 0: never written to the calendar
	CalendarSyncedAt       time.Time
	DeletedAt              time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// Restore rebuilds a stored lesson without side effects; it only checks that the state is consistent.
func Restore(s State) (*Lesson, error) {
	switch {
	case s.Version < 1:
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidLesson, s.Version)
	case s.ContentRevision < 1:
		return nil, fmt.Errorf("%w: content revision %d", ErrInvalidLesson, s.ContentRevision)
	case s.CalendarSyncedRevision < 0 || s.CalendarSyncedRevision > s.ContentRevision:
		return nil, fmt.Errorf("%w: calendar revision %d of content revision %d", ErrInvalidLesson,
			s.CalendarSyncedRevision, s.ContentRevision)
	}

	l := &Lesson{
		version: s.Version, id: s.ID, content: s.Content, sourceUID: s.SourceUID, startNotifiedAt: s.StartNotifiedAt,
		contentRevision: s.ContentRevision, syncedRevision: s.CalendarSyncedRevision, syncedAt: s.CalendarSyncedAt,
		deletedAt: s.DeletedAt, createdAt: s.CreatedAt, updatedAt: s.UpdatedAt,
	}
	if err := l.validate(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Lesson) validate() error {
	if err := l.id.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidLesson, err)
	}
	if err := l.content.Validate(); err != nil {
		return err
	}
	if err := ValidateSourceUID(l.sourceUID); err != nil {
		return err
	}
	if l.createdAt.IsZero() || l.updatedAt.IsZero() {
		return fmt.Errorf("%w: missing time", ErrInvalidLesson)
	}
	return nil
}

// ChangeContent replaces what the lesson shows. A real change bumps the content revision, so the calendar projection
// becomes pending; a new start time also clears StartNotifiedAt. The same content changes nothing and reports false.
func (l *Lesson) ChangeContent(c Content, now time.Time) (changed bool, err error) {
	switch {
	case l.Deleted():
		return false, ErrDeleted
	case now.IsZero():
		return false, fmt.Errorf("%w: no change time", ErrInvalidLesson)
	}
	if err := c.Validate(); err != nil {
		return false, err
	}
	if c.Equal(l.content) {
		return false, nil
	}

	// A moved lesson starts again later, so its "lesson is starting" prompt must be raised again (PRD 6.1).
	if !c.Start.Equal(l.content.Start) {
		l.startNotifiedAt = time.Time{}
	}
	l.content = c
	l.contentRevision++
	l.updatedAt = now
	return true, nil
}

// MarkDeleted removes the lesson from the schedule softly: the row stays until its calendar event is deleted
// (ADR-0004(e)). The deletion is a change the calendar must see, so the content revision grows.
func (l *Lesson) MarkDeleted(now time.Time) error {
	switch {
	case l.Deleted():
		return ErrDeleted
	case now.IsZero():
		return fmt.Errorf("%w: no deletion time", ErrInvalidLesson)
	}

	l.deletedAt = now
	l.contentRevision++
	l.updatedAt = now
	return nil
}

// CalendarPending reports whether the calendar projection is out of date: the lesson is deleted (until purged), was
// never written, or changed after the last write. The sqlite query of ListPendingCalendarSync states the same rule.
func (l *Lesson) CalendarPending() bool {
	return l.Deleted() || l.syncedRevision == 0 || l.syncedRevision < l.contentRevision
}

// MarkCalendarSynced records that content revision revision was written to the calendar at at (ADR-0004(e)). The
// revision is the one the writer read before the write, so it lies in [1, ContentRevision]; a lesson changed since
// then stays pending. An older revision than the one already recorded is ignored: a slow writer must not move the
// projection back. A deleted lesson is refused: it stays pending until its event is deleted and the row purged.
func (l *Lesson) MarkCalendarSynced(revision int64, at time.Time) error {
	switch {
	case l.Deleted():
		return ErrDeleted
	case at.IsZero():
		return fmt.Errorf("%w: no calendar write time", ErrInvalidLesson)
	case revision < 1 || revision > l.contentRevision:
		return fmt.Errorf("%w: calendar revision %d of content revision %d", ErrInvalidLesson, revision, l.contentRevision)
	case revision < l.syncedRevision:
		return nil
	}

	l.syncedRevision = revision
	l.syncedAt = at
	return nil
}

// ForgetCalendarProjection marks the lesson as never written to the calendar, so it becomes pending: the calendar it
// was written to is gone or new (a fresh calendar holds none of the events). It reports whether anything changed.
func (l *Lesson) ForgetCalendarProjection() bool {
	if l.syncedRevision == 0 && l.syncedAt.IsZero() {
		return false
	}
	l.syncedRevision = 0
	l.syncedAt = time.Time{}
	return true
}

// ID returns the lesson identifier.
func (l *Lesson) ID() shared.LessonID { return l.id }

// Content returns what the lesson shows.
func (l *Lesson) Content() Content { return l.content }

// SourceUID returns the identity of the lesson in the imported file; empty for a manual lesson.
func (l *Lesson) SourceUID() string { return l.sourceUID }

// StartNotifiedAt returns when the "lesson is starting" prompt was raised; zero if it was not.
func (l *Lesson) StartNotifiedAt() time.Time { return l.startNotifiedAt }

// ContentRevision returns the revision of what the calendar shows; it starts at 1.
func (l *Lesson) ContentRevision() int64 { return l.contentRevision }

// CalendarSyncedRevision returns the content revision last written to the calendar; 0 if never written.
func (l *Lesson) CalendarSyncedRevision() int64 { return l.syncedRevision }

// CalendarSyncedAt returns when the calendar was last written; informational only.
func (l *Lesson) CalendarSyncedAt() time.Time { return l.syncedAt }

// Deleted reports whether the lesson is soft-deleted.
func (l *Lesson) Deleted() bool { return !l.deletedAt.IsZero() }

// DeletedAt returns when the lesson was removed from the schedule; zero while it is live.
func (l *Lesson) DeletedAt() time.Time { return l.deletedAt }

// CreatedAt returns the creation time.
func (l *Lesson) CreatedAt() time.Time { return l.createdAt }

// UpdatedAt returns the time of the last change.
func (l *Lesson) UpdatedAt() time.Time { return l.updatedAt }

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (l *Lesson) Version() int64 { return l.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (l *Lesson) Persisted(version int64) { l.version = version }
