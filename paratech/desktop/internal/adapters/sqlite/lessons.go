package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// LessonRepository stores the local schedule (the schedulesync.LessonRepository port, PRD section 11). Lessons are
// deleted softly (ADR-0004(e)): the repository never issues a DELETE except in PurgeDeleted.
type LessonRepository struct{ db *DB }

// NewLessonRepository returns the lesson repository of db.
func NewLessonRepository(db *DB) *LessonRepository { return &LessonRepository{db: db} }

// Get loads a live lesson; a soft-deleted or missing one is shared.ErrNotFound.
func (r *LessonRepository) Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	row, err := r.db.Queries(ctx).GetLesson(ctx, string(id))
	if err != nil {
		return nil, MapError(ctx, err)
	}
	return lessonFromRow(row)
}

// Save writes one lesson with the version compare-and-swap of ADR-0004(d).
func (r *LessonRepository) Save(ctx context.Context, l *lesson.Lesson) error {
	return r.ApplyChanges(ctx, []*lesson.Lesson{l}, nil)
}

// ApplyChanges writes the batch of an import commit in one transaction (joining an outer Do), every lesson with the
// version compare-and-swap; any failure rolls the whole batch back. Deleted lessons must already be marked deleted
// and are written as soft-deleted rows. Persisted is called on every lesson only after the batch succeeded.
func (r *LessonRepository) ApplyChanges(ctx context.Context, upserts, deleted []*lesson.Lesson) error {
	seen := make(map[shared.LessonID]bool, len(upserts)+len(deleted))
	for _, l := range append(append([]*lesson.Lesson(nil), upserts...), deleted...) {
		switch {
		case l == nil:
			return errors.New("sqlite: ApplyChanges with a nil lesson")
		case seen[l.ID()]:
			return fmt.Errorf("sqlite: ApplyChanges with lesson %s twice", l.ID())
		}
		seen[l.ID()] = true
	}
	for _, l := range deleted {
		if !l.Deleted() {
			return fmt.Errorf("sqlite: ApplyChanges: lesson %s is not marked deleted", l.ID())
		}
	}

	// Deletions first: they free the live source uids that a new row of the same batch may take.
	batch := append(append([]*lesson.Lesson(nil), deleted...), upserts...)
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		for _, l := range batch {
			if err := saveLesson(ctx, q, l); err != nil {
				return fmt.Errorf("lesson %s: %w", l.ID(), err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Inside an outer Tx, Do joins it: an outer rollback undoes the rows but not this, so reload (ADR-0004(d)).
	for _, l := range batch {
		l.Persisted(l.Version() + 1)
	}
	return nil
}

func saveLesson(ctx context.Context, q *sqlcgen.Queries, l *lesson.Lesson) error {
	p := lessonUpdateParams(l)
	if l.Version() == 0 {
		return DuplicateAsVersionConflict(MapError(ctx, q.InsertLesson(ctx, sqlcgen.InsertLessonParams{
			ID: p.ID, Title: p.Title, StartsAt: p.StartsAt, EndsAt: p.EndsAt, Timezone: p.Timezone,
			Subject: p.Subject, Kind: p.Kind, Teacher: p.Teacher, CallLink: p.CallLink, SourceUid: p.SourceUid, StartNotifiedAt: p.StartNotifiedAt, ContentRevision: p.ContentRevision,
			CalendarSyncedRevision: p.CalendarSyncedRevision, CalendarSyncedAt: p.CalendarSyncedAt,
			DeletedAt: p.DeletedAt, CreatedAt: jobMs(l.CreatedAt()), UpdatedAt: p.UpdatedAt,
		})))
	}
	n, err := q.UpdateLesson(ctx, p)
	return VersionCAS(ctx, n, err)
}

// ListLive returns the live lessons ordered by start.
func (r *LessonRepository) ListLive(ctx context.Context) ([]*lesson.Lesson, error) {
	rows, err := r.db.Queries(ctx).ListLiveLessons(ctx)
	return lessonsFromRows(ctx, rows, err)
}

// ListStartingBetween returns the live lessons starting in [from, to), ordered by start.
func (r *LessonRepository) ListStartingBetween(ctx context.Context, from, to time.Time) ([]*lesson.Lesson, error) {
	rows, err := r.db.Queries(ctx).ListLessonsStartingBetween(ctx, sqlcgen.ListLessonsStartingBetweenParams{
		FromMs: jobMs(from), ToMs: jobMs(to),
	})
	return lessonsFromRows(ctx, rows, err)
}

// ListPendingCalendarSync returns up to limit lessons whose calendar projection is out of date, deleted ones included.
func (r *LessonRepository) ListPendingCalendarSync(ctx context.Context, limit int) ([]*lesson.Lesson, error) {
	if limit <= 0 {
		return []*lesson.Lesson{}, nil
	}
	rows, err := r.db.Queries(ctx).ListLessonsPendingCalendarSync(ctx, int64(limit))
	return lessonsFromRows(ctx, rows, err)
}

// PurgeDeleted removes the row of a soft-deleted lesson. A missing row is not an error; a live lesson is
// shared.ErrConflict and stays. Materials linked to the lesson lose the link by FK cascade, so their version is
// bumped in the same transaction.
func (r *LessonRepository) PurgeDeleted(ctx context.Context, id shared.LessonID) error {
	return r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if err := q.BumpMaterialsLinkedToDeletedLesson(ctx, string(id)); err != nil {
			return MapError(ctx, err)
		}
		n, err := q.PurgeDeletedLesson(ctx, string(id))
		if err != nil {
			return MapError(ctx, err)
		}
		if n > 0 {
			return nil
		}
		exists, err := q.LessonExists(ctx, string(id))
		switch {
		case err != nil:
			return MapError(ctx, err)
		case exists:
			return fmt.Errorf("sqlite: purge of live lesson %s: %w", id, shared.ErrConflict)
		}
		return nil
	})
}

func lessonsFromRows(ctx context.Context, rows []sqlcgen.ScheduleEntry, err error) ([]*lesson.Lesson, error) {
	if err != nil {
		return nil, MapError(ctx, err)
	}
	out := make([]*lesson.Lesson, 0, len(rows))
	for _, row := range rows {
		l, err := lessonFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func lessonFromRow(row sqlcgen.ScheduleEntry) (*lesson.Lesson, error) {
	kind, err := lesson.KindOf(row.Kind)
	if err != nil {
		return nil, fmt.Errorf("sqlite: stored lesson %s: %w", row.ID, err)
	}
	l, err := lesson.Restore(lesson.State{
		Version: row.Version,
		ID:      shared.LessonID(row.ID),
		Content: lesson.Content{
			Title: row.Title, Start: jobFromMs(row.StartsAt), End: jobFromMs(row.EndsAt), TimeZone: row.Timezone,
			Subject: row.Subject, Kind: kind, Teacher: row.Teacher, CallLink: row.CallLink,
		},
		SourceUID:              row.SourceUid.String,
		StartNotifiedAt:        jobFromNullMs(row.StartNotifiedAt),
		ContentRevision:        row.ContentRevision,
		CalendarSyncedRevision: row.CalendarSyncedRevision.Int64,
		CalendarSyncedAt:       jobFromNullMs(row.CalendarSyncedAt),
		DeletedAt:              jobFromNullMs(row.DeletedAt),
		CreatedAt:              jobFromMs(row.CreatedAt),
		UpdatedAt:              jobFromMs(row.UpdatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite: stored lesson %s: %w", row.ID, err)
	}
	return l, nil
}

func lessonUpdateParams(l *lesson.Lesson) sqlcgen.UpdateLessonParams {
	c := l.Content()
	var synced sql.NullInt64
	if rev := l.CalendarSyncedRevision(); rev > 0 {
		synced = sql.NullInt64{Int64: rev, Valid: true}
	}
	return sqlcgen.UpdateLessonParams{
		ID:                     string(l.ID()),
		ExpectedVersion:        l.Version(),
		Title:                  c.Title,
		StartsAt:               jobMs(c.Start),
		EndsAt:                 jobMs(c.End),
		Timezone:               c.TimeZone,
		Subject:                c.Subject,
		Kind:                   c.Kind.String(),
		Teacher:                c.Teacher,
		CallLink:               c.CallLink,
		SourceUid:              jobNullString(l.SourceUID()),
		StartNotifiedAt:        jobNullMs(l.StartNotifiedAt()),
		ContentRevision:        l.ContentRevision(),
		CalendarSyncedRevision: synced,
		CalendarSyncedAt:       jobNullMs(l.CalendarSyncedAt()),
		DeletedAt:              jobNullMs(l.DeletedAt()),
		UpdatedAt:              jobMs(l.UpdatedAt()),
	}
}
