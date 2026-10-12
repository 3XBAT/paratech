package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// claimRetries bounds the candidates tried by one Claim when the guarded update loses a race.
const claimRetries = 8

// JobRepository stores jobs (the jobrunner.JobRepository port). The status machine stays in job.Job: the repository
// restores the aggregate, lets it make the transition and only persists the result.
type JobRepository struct{ db *DB }

// NewJobRepository returns the job repository of db.
func NewJobRepository(db *DB) *JobRepository { return &JobRepository{db: db} }

// Enqueue stores a new job. A job with the same idempotency key makes it a no-op that returns the existing id.
func (r *JobRepository) Enqueue(ctx context.Context, j *job.Job) (shared.JobID, bool, error) {
	if j.Version() != 0 {
		return "", false, fmt.Errorf("sqlite: enqueue of a stored job %s", j.ID())
	}
	id, created := j.ID(), false
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		err := MapError(ctx, q.InsertJob(ctx, jobInsertParams(j)))
		switch {
		case err == nil:
			created = true
			return nil
		case errors.Is(err, shared.ErrDuplicate) && !IsPrimaryKeyViolation(err):
			existing, gerr := q.GetJobIDByKey(ctx, j.Key())
			if gerr != nil {
				return MapError(ctx, gerr)
			}
			id = shared.JobID(existing)
			return nil
		}
		return DuplicateAsVersionConflict(err)
	})
	if err != nil {
		return "", false, err
	}
	if created {
		j.Persisted(1)
	}
	return id, created, nil
}

// Get loads a job by id.
func (r *JobRepository) Get(ctx context.Context, id shared.JobID) (*job.Job, error) {
	row, err := r.db.Queries(ctx).GetJob(ctx, string(id))
	if err != nil {
		return nil, MapError(ctx, err)
	}
	return jobFromRow(row)
}

// Save writes the job: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)).
func (r *JobRepository) Save(ctx context.Context, j *job.Job) error {
	next := j.Version() + 1
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if j.Version() == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertJob(ctx, jobInsertParams(j))))
		}
		n, err := q.UpdateJob(ctx, sqlcgen.UpdateJobParams(jobUpdateParams(j)))
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return err
	}
	j.Persisted(next)
	return nil
}

// Claim takes the next due QUEUED job admitted by filter and returns it RUNNING. The write transaction and the
// guarded UPDATE (status and version) each keep two workers from receiving the same job.
func (r *JobRepository) Claim(ctx context.Context, now time.Time, filter job.ClaimFilter) (*job.Job, bool, error) {
	if filter.IsZero() {
		return nil, false, nil
	}
	types, err := jobTypeList(filter.Types())
	if err != nil {
		return nil, false, err
	}
	heavy, err := jobTypeList(filter.HeavyTypes())
	if err != nil {
		return nil, false, err
	}
	var heavyAllowed int64
	if filter.HeavyAllowed() {
		heavyAllowed = 1
	}
	query := sqlcgen.NextClaimableJobParams{Now: jobMs(now), Types: types, HeavyAllowed: heavyAllowed, HeavyTypes: heavy}

	var claimed *job.Job
	err = r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		for range claimRetries {
			row, err := q.NextClaimableJob(ctx, query)
			if err != nil {
				if err = MapError(ctx, err); errors.Is(err, shared.ErrNotFound) {
					return nil // nothing eligible
				}
				return err
			}
			j, err := jobFromRow(row)
			if err != nil {
				return err
			}
			if err := j.Claim(now); err != nil {
				return fmt.Errorf("sqlite: claim job %s: %w", j.ID(), err)
			}
			n, err := q.UpdateClaimedJob(ctx, sqlcgen.UpdateClaimedJobParams(jobUpdateParams(j)))
			if err = MapError(ctx, err); err != nil {
				return err
			}
			if n == 1 {
				claimed = j
				return nil
			}
			// Lost the race for this candidate: try the next one.
		}
		return shared.ErrVersionConflict
	})
	if err != nil {
		return nil, false, err
	}
	if claimed == nil {
		return nil, false, nil
	}
	claimed.Persisted(claimed.Version() + 1)
	return claimed, true, nil
}

// ListByStatus returns the jobs in any of the statuses, oldest first.
func (r *JobRepository) ListByStatus(ctx context.Context, statuses ...job.Status) ([]*job.Job, error) {
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = string(s)
	}
	rows, err := r.db.Queries(ctx).ListJobsByStatus(ctx, names)
	if err != nil {
		return nil, MapError(ctx, err)
	}
	out := make([]*job.Job, 0, len(rows))
	for _, row := range rows {
		j, err := jobFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}

func jobTypeList(types []job.Type) (string, error) {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = string(t)
	}
	b, err := json.Marshal(names)
	return string(b), err
}

func jobFromRow(row sqlcgen.Job) (*job.Job, error) {
	j, err := job.Restore(job.State{
		Version:       row.Version,
		ID:            shared.JobID(row.ID),
		Type:          job.Type(row.Type),
		Status:        job.Status(row.Status),
		Key:           row.IdempotencyKey,
		Payload:       row.Payload,
		PinnedModelID: shared.SttModelID(row.PinnedModelID.String),
		Attempts:      int(row.Attempts),
		MaxAttempts:   int(row.MaxAttempts),
		NextRunAt:     jobFromMs(row.NextRunAt),
		RunNow:        row.RunNow == 1,
		Percent:       int(row.ProgressPercent),
		Checkpoint:    row.Checkpoint.String,
		LastError:     row.LastError.String,
		CreatedAt:     jobFromMs(row.CreatedAt),
		StartedAt:     jobFromNullMs(row.StartedAt),
		FinishedAt:    jobFromNullMs(row.FinishedAt),
		UpdatedAt:     jobFromMs(row.UpdatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite: stored job %s: %w", row.ID, err)
	}
	return j, nil
}

func jobInsertParams(j *job.Job) sqlcgen.InsertJobParams {
	p := j.Progress()
	return sqlcgen.InsertJobParams{
		ID:              string(j.ID()),
		Type:            string(j.Type()),
		Status:          string(j.Status()),
		IdempotencyKey:  j.Key(),
		Payload:         j.Payload(),
		PinnedModelID:   jobNullString(string(j.PinnedModelID())),
		Attempts:        int64(j.Attempts()),
		MaxAttempts:     int64(j.MaxAttempts()),
		NextRunAt:       jobMs(j.NextRunAt()),
		RunNow:          jobBoolInt(j.RunNow()),
		ProgressPercent: int64(p.Percent()),
		Checkpoint:      jobNullString(p.Checkpoint()),
		LastError:       jobNullString(j.LastError()),
		CreatedAt:       jobMs(j.CreatedAt()),
		StartedAt:       jobNullMs(j.StartedAt()),
		FinishedAt:      jobNullMs(j.FinishedAt()),
		UpdatedAt:       jobMs(j.UpdatedAt()),
	}
}

// jobUpdateParams holds the mutable columns; the claimed variant has the same fields, so the callers convert it.
func jobUpdateParams(j *job.Job) sqlcgen.UpdateJobParams {
	p := j.Progress()
	return sqlcgen.UpdateJobParams{
		ID:              string(j.ID()),
		ExpectedVersion: j.Version(),
		Status:          string(j.Status()),
		Attempts:        int64(j.Attempts()),
		NextRunAt:       jobMs(j.NextRunAt()),
		RunNow:          jobBoolInt(j.RunNow()),
		ProgressPercent: int64(p.Percent()),
		Checkpoint:      jobNullString(p.Checkpoint()),
		LastError:       jobNullString(j.LastError()),
		StartedAt:       jobNullMs(j.StartedAt()),
		FinishedAt:      jobNullMs(j.FinishedAt()),
		UpdatedAt:       jobMs(j.UpdatedAt()),
	}
}

func jobMs(t time.Time) int64 { return t.UnixMilli() }

func jobFromMs(v int64) time.Time { return time.UnixMilli(v).UTC() }

func jobNullMs(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: jobMs(t), Valid: true}
}

func jobFromNullMs(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return jobFromMs(v.Int64)
}

func jobNullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func jobBoolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
