package sqlite

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/agent/post"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the post repository, used in its error messages.
const (
	opGetTodayPost   = "sqlite: get today post"
	opSaveTodayPost  = "sqlite: save today post"
	opListTodayPosts = "sqlite: list today posts"
)

// TodayPostRepository stores the posts after lessons (the todayfeed.PostRepository port, TASK-125). The rules stay in
// post.Post: the repository only converts rows, and Restore refuses a row that does not fit them.
type TodayPostRepository struct{ db *DB }

// NewTodayPostRepository returns the post repository of db.
func NewTodayPostRepository(db *DB) *TodayPostRepository { return &TodayPostRepository{db: db} }

// Get loads the post of a recording; a missing one wraps shared.ErrNotFound.
func (r *TodayPostRepository) Get(ctx context.Context, rec shared.RecordingID) (*post.Post, error) {
	row, err := r.db.Queries(ctx).GetTodayPost(ctx, string(rec))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetTodayPost, rec, MapError(ctx, err))
	}
	p, err := todayPostFromRow(row)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetTodayPost, rec, err)
	}
	return p, nil
}

// Save writes the post: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). A post of the
// recording that exists already, or a stale version, yields shared.ErrVersionConflict; a recording that does not
// exist yields an error wrapping shared.ErrConflict (foreign key).
func (r *TodayPostRepository) Save(ctx context.Context, p *post.Post) error {
	st := p.State()
	if st.Version < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveTodayPost, st.Version)
	}

	next := st.Version + 1
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertTodayPost(ctx, sqlcgen.InsertTodayPostParams{
				RecordingID: string(st.Recording), TranscriptID: string(st.Transcript), SummaryAt: jobNullMs(st.SummaryAt),
				HomeworkAt: jobNullMs(st.HomeworkAt), CreatedAt: jobMs(st.CreatedAt), UpdatedAt: jobMs(st.UpdatedAt),
			})))
		}
		n, err := q.UpdateTodayPost(ctx, sqlcgen.UpdateTodayPostParams{
			TranscriptID: string(st.Transcript), SummaryAt: jobNullMs(st.SummaryAt), HomeworkAt: jobNullMs(st.HomeworkAt),
			UpdatedAt: jobMs(st.UpdatedAt), RecordingID: string(st.Recording), ExpectedVersion: st.Version,
		})
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveTodayPost, st.Recording, err)
	}

	p.Persisted(next)
	return nil
}

// ListUpdatedSince returns the posts changed at or after since, newest first, at most limit (at least 1).
func (r *TodayPostRepository) ListUpdatedSince(ctx context.Context, since time.Time, limit int) ([]*post.Post, error) {
	if limit < 1 {
		return nil, fmt.Errorf("%s: limit %d", opListTodayPosts, limit)
	}
	rows, err := r.db.Queries(ctx).ListTodayPostsSince(ctx, sqlcgen.ListTodayPostsSinceParams{
		Since: jobMs(since), MaxRows: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListTodayPosts, MapError(ctx, err))
	}
	out := make([]*post.Post, 0, len(rows))
	for _, row := range rows {
		p, err := todayPostFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", opListTodayPosts, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func todayPostFromRow(row sqlcgen.TodayPost) (*post.Post, error) {
	return post.Restore(post.State{
		Version: row.Version, Recording: shared.RecordingID(row.RecordingID), Transcript: shared.TranscriptID(row.TranscriptID),
		SummaryAt: jobFromNullMs(row.SummaryAt), HomeworkAt: jobFromNullMs(row.HomeworkAt),
		CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	})
}
