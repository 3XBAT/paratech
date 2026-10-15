package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// claimRetries bounds the candidates tried by one Claim when the guarded update loses a race.
const claimRetries = 8

// scanErrorPrefix starts the message of database/sql when a column value cannot be converted (pinned by a test).
const scanErrorPrefix = "sql: Scan error"

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

// HasKeyPrefix reports whether a job of type typ in any status has an idempotency key that starts with prefix; an
// empty prefix matches every job of the type.
func (r *JobRepository) HasKeyPrefix(ctx context.Context, typ job.Type, prefix string) (bool, error) {
	found, err := r.db.Queries(ctx).HasJobKeyPrefix(ctx, sqlcgen.HasJobKeyPrefixParams{Type: string(typ), Prefix: prefix})
	if err != nil {
		return false, MapError(ctx, err)
	}
	return found, nil
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

// Claim takes the next due QUEUED job admitted by filter and returns it RUNNING. An idle poll only reads; the write
// transaction starts when a candidate exists. The write transaction and the guarded UPDATE (status and version) each
// keep two workers from receiving the same job.
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

	// Cheap read without the write slot; the candidate is looked up again inside the transaction.
	peek, err := nextClaimCandidate(ctx, r.db.Queries(ctx), query, now, nil)
	if err != nil || peek == nil {
		return nil, false, err
	}

	var claimed *job.Job
	err = r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		skip := []string{}
		for range claimRetries {
			j, err := nextClaimCandidate(ctx, q, query, now, &skip)
			if err != nil || j == nil {
				return err // nothing eligible any more
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

// nextClaimCandidate returns the first stored job that can be claimed, already moved to RUNNING in memory, or nil if
// there is none. A row that cannot be restored or claimed is corrupt: its id goes to skip and the next row is tried,
// so one bad row never starves the queue behind it. skip may be nil; the query is not changed.
func nextClaimCandidate(ctx context.Context, q *sqlcgen.Queries, query sqlcgen.NextClaimableJobParams, now time.Time, skip *[]string) (*job.Job, error) {
	local := []string{} // never nil: JSON null would make NOT IN match nothing
	if skip == nil {
		skip = &local
	}
	for {
		ids, err := json.Marshal(*skip)
		if err != nil {
			return nil, err
		}
		query.SkipIds = string(ids)
		row, err := q.NextClaimableJob(ctx, query)
		if err != nil {
			if err = MapError(ctx, err); errors.Is(err, shared.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		if j, err := jobFromRow(row); err == nil && j.Claim(now) == nil {
			return j, nil
		}
		*skip = append(*skip, row.ID) // each pass adds an id, so the loop ends
	}
}

// ListByStatus returns the jobs in any of the statuses, oldest first.
func (r *JobRepository) ListByStatus(ctx context.Context, statuses ...job.Status) ([]*job.Job, error) {
	rows, err := r.db.Queries(ctx).ListJobsByStatus(ctx, jobStatusNames(statuses))
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

// ListActivity lists the jobs of the jobs screen: every job in a non-terminal status (so a waiting job never falls
// off the list) plus the newest maxTerminal others, newest first by creation time, then id; a maxTerminal below 1
// adds no terminal job. A row that cannot be read or restored does not fail the listing: it is left out and its id
// goes to unreadable. A job that disappeared between the two reads is skipped. Other database errors fail the call.
func (r *JobRepository) ListActivity(ctx context.Context, maxTerminal int) ([]*job.Job, []shared.JobID, error) {
	active, err := json.Marshal(jobStatusNames(job.ActiveStatuses()))
	if err != nil {
		return nil, nil, err
	}
	q := r.db.Queries(ctx)
	ids, err := q.ListJobActivityIDs(ctx, sqlcgen.ListJobActivityIDsParams{
		Active: string(active), MaxTerminal: int64(max(maxTerminal, 0)), // SQLite reads a negative LIMIT as no limit
	})
	if err != nil {
		return nil, nil, MapError(ctx, err)
	}

	jobs, unreadable := make([]*job.Job, 0, len(ids)), []shared.JobID{}
	for _, id := range ids {
		row, err := q.GetJob(ctx, id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			continue
		case isScanError(err):
			unreadable = append(unreadable, shared.JobID(id))
			continue
		case err != nil:
			return nil, nil, MapError(ctx, err)
		}
		j, err := jobFromRow(row)
		if err != nil {
			unreadable = append(unreadable, shared.JobID(id))
			continue
		}
		jobs = append(jobs, j)
	}
	return jobs, unreadable, nil
}

// ListByStatusTolerant is ListByStatus for startup recovery: a row that cannot be read or restored does not fail the
// listing, its id goes to corrupt and the row is left unchanged. A row with a NULL id has no id to report: it is left
// out and their number is logged once per call. A job that left the statuses or disappeared between the two reads is
// skipped. Other database errors fail the whole call.
func (r *JobRepository) ListByStatusTolerant(ctx context.Context, statuses ...job.Status) ([]*job.Job, []shared.JobID, error) {
	q, names := r.db.Queries(ctx), jobStatusNames(statuses)
	ids, err := q.ListJobIDsByStatus(ctx, names)
	if err != nil {
		return nil, nil, MapError(ctx, err)
	}

	noID, err := q.CountJobsWithoutIDByStatus(ctx, names)
	if err != nil {
		return nil, nil, MapError(ctx, err)
	}
	if noID > 0 {
		r.db.logger.Warn("job rows without an id are left out of the listing", slog.Int64("count", noID))
	}

	jobs, corrupt := make([]*job.Job, 0, len(ids)), []shared.JobID{}
	for _, id := range ids {
		row, err := q.GetJob(ctx, id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			continue
		case isScanError(err):
			corrupt = append(corrupt, shared.JobID(id))
			continue
		case err != nil:
			return nil, nil, MapError(ctx, err)
		}

		j, err := jobFromRow(row)
		if err != nil {
			corrupt = append(corrupt, shared.JobID(id))
			continue
		}
		if slices.Contains(statuses, j.Status()) {
			jobs = append(jobs, j)
		}
	}
	return jobs, corrupt, nil
}

// isScanError reports a column value that database/sql could not convert, such as text in an INTEGER column of a
// tampered row. database/sql exports no type for it, so the stable message prefix is the handle.
func isScanError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), scanErrorPrefix)
}

func jobStatusNames(statuses []job.Status) []string {
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = string(s)
	}
	return names
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
