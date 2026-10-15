package sqlite

import (
	"context"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/notice"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the notice repository, used in its error messages.
const (
	opRecordNotice = "sqlite: record job notice"
	opGetNotice    = "sqlite: get job notice"
)

// NoticeRepository stores the notices of finished processing (the notify.NoticeRepository port, PRD section 15). A
// notice is written once and never changed; validation stays in notice.Notice.
type NoticeRepository struct{ db *DB }

// NewNoticeRepository returns the notice repository of db.
func NewNoticeRepository(db *DB) *NoticeRepository { return &NoticeRepository{db: db} }

// Record stores the notice unless one for the same job and status exists; first is false in that case. A job that
// does not exist yields an error wrapping shared.ErrConflict (foreign key).
func (r *NoticeRepository) Record(ctx context.Context, n *notice.Notice) (bool, error) {
	if n == nil {
		return false, fmt.Errorf("%s: no notice", opRecordNotice)
	}

	var rows int64
	err := r.db.Do(ctx, func(ctx context.Context) error {
		var err error
		rows, err = r.db.Queries(ctx).InsertJobNotice(ctx, sqlcgen.InsertJobNoticeParams{
			JobID: string(n.Job()), Status: string(n.Status()), JobType: string(n.Type()), NotifiedAt: jobMs(n.NotifiedAt()),
		})
		return MapError(ctx, err)
	})
	if err != nil {
		return false, fmt.Errorf("%s %s %s: %w", opRecordNotice, n.Job(), n.Status(), err)
	}
	return rows == 1, nil
}

// Get loads the notice of a job and status; a missing one wraps shared.ErrNotFound.
func (r *NoticeRepository) Get(ctx context.Context, id shared.JobID, status job.Status) (*notice.Notice, error) {
	row, err := r.db.Queries(ctx).GetJobNotice(ctx, sqlcgen.GetJobNoticeParams{JobID: string(id), Status: string(status)})
	if err != nil {
		return nil, fmt.Errorf("%s %s %s: %w", opGetNotice, id, status, MapError(ctx, err))
	}

	n, err := notice.Restore(notice.State{
		Job: shared.JobID(row.JobID), Type: job.Type(row.JobType), Status: job.Status(row.Status), NotifiedAt: jobFromMs(row.NotifiedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("%s %s %s: stored row: %w", opGetNotice, id, status, err)
	}
	return n, nil
}
