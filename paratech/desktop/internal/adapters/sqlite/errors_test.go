package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/shared"
)

func insertParams(updatedAt int64) sqlcgen.InsertSettingsParams {
	return sqlcgen.InsertSettingsParams{
		RecordingPromptsEnabled:    1,
		ProcessOnlyOnAcPower:       1,
		NotifyOnProcessingComplete: 1,
		UpdatedAt:                  updatedAt,
	}
}

func TestMapErrorWithRealDriverErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		run     func(db *DB) error
		is      []error
		isNot   []error
		pk      bool   // IsPrimaryKeyViolation
		wantMsg string // part of the message; the table and column help diagnosing
	}{
		{
			name: "unique constraint",
			run: func(db *DB) error {
				_, err := db.sql.ExecContext(ctx, `INSERT INTO jobs (id, type, status, idempotency_key, max_attempts, next_run_at, created_at, updated_at)
					VALUES ('j2', 'stt', 'QUEUED', 'key-1', 3, 0, 0, 0)`)
				return err
			},
			is:      []error{shared.ErrDuplicate, shared.ErrConflict},
			isNot:   []error{shared.ErrVersionConflict, ErrConstraint, ErrBusy},
			wantMsg: "jobs.idempotency_key",
		},
		{
			name: "primary key",
			run: func(db *DB) error {
				_, err := db.sql.ExecContext(ctx, `INSERT INTO jobs (id, type, status, idempotency_key, max_attempts, next_run_at, created_at, updated_at)
					VALUES ('j1', 'stt', 'QUEUED', 'key-2', 3, 0, 0, 0)`)
				return err
			},
			is:    []error{shared.ErrDuplicate, shared.ErrConflict},
			isNot: []error{shared.ErrVersionConflict},
			pk:    true,
		},
		{
			name: "integer primary key (rowid alias)",
			run: func(db *DB) error {
				if _, err := db.sql.ExecContext(ctx, `INSERT INTO settings (id, updated_at) VALUES (1, 0)`); err != nil {
					return err
				}
				_, err := db.sql.ExecContext(ctx, `INSERT INTO settings (id, updated_at) VALUES (1, 0)`)
				return err
			},
			is: []error{shared.ErrDuplicate},
			pk: true,
		},
		{
			name: "foreign key",
			run: func(db *DB) error {
				_, err := db.sql.ExecContext(ctx, `INSERT INTO assignments (id, lesson_id, title, created_at, updated_at) VALUES ('a1', 'nope', 'hw', 0, 0)`)
				return err
			},
			is:    []error{shared.ErrConflict},
			isNot: []error{shared.ErrDuplicate, shared.ErrVersionConflict, ErrConstraint},
		},
		{
			name: "restrict on parent delete",
			run: func(db *DB) error {
				if _, err := db.sql.ExecContext(ctx, `INSERT INTO stt_models (id, name, model_version, file_ref, size_bytes, sha256, installed_at)
					VALUES ('m1', 'small', '1', 'models/small.bin', 10, '`+strings.Repeat("a", 64)+`', 0)`); err != nil {
					return err
				}
				if _, err := db.sql.ExecContext(ctx, `INSERT INTO settings (id, default_stt_model_id, updated_at) VALUES (1, 'm1', 0)`); err != nil {
					return err
				}
				_, err := db.sql.ExecContext(ctx, `DELETE FROM stt_models WHERE id = 'm1'`)
				return err
			},
			is:    []error{shared.ErrConflict},
			isNot: []error{shared.ErrDuplicate, shared.ErrVersionConflict, ErrConstraint},
		},
		{
			name: "check constraint",
			run: func(db *DB) error {
				_, err := db.sql.ExecContext(ctx, `UPDATE jobs SET status = 'BOGUS' WHERE id = 'j1'`)
				return err
			},
			is:    []error{ErrConstraint},
			isNot: []error{shared.ErrConflict, shared.ErrDuplicate},
		},
		{
			name: "not null constraint",
			run: func(db *DB) error {
				_, err := db.sql.ExecContext(ctx, `UPDATE jobs SET type = NULL WHERE id = 'j1'`)
				return err
			},
			is: []error{ErrConstraint},
		},
		{
			name: "no rows",
			run: func(db *DB) error {
				var id string
				return db.sql.QueryRowContext(ctx, "SELECT id FROM jobs WHERE id = 'missing'").Scan(&id)
			},
			is: []error{shared.ErrNotFound},
		},
		{
			name: "syntax error is a generic database error",
			run: func(db *DB) error {
				_, err := db.sql.ExecContext(ctx, `SELEKT 1`)
				return err
			},
			is:    []error{ErrDatabase},
			isNot: []error{shared.ErrConflict, ErrBusy, ErrConstraint},
		},
		{
			name: "non-driver error",
			run:  func(*DB) error { return errors.New("something else") },
			is:   []error{ErrDatabase},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openTest(t, nil)
			mustExec(t, db, `INSERT INTO jobs (id, type, status, idempotency_key, max_attempts, next_run_at, created_at, updated_at)
				VALUES ('j1', 'stt', 'QUEUED', 'key-1', 3, 0, 0, 0)`)
			raw := tc.run(db)
			if raw == nil {
				t.Fatal("expected a database error")
			}
			got := MapError(ctx, raw)
			if IsPrimaryKeyViolation(got) != tc.pk {
				t.Errorf("IsPrimaryKeyViolation(%v) = %v, want %v", got, !tc.pk, tc.pk)
			}
			for _, want := range tc.is {
				if !errors.Is(got, want) {
					t.Errorf("errors.Is(%v, %v) = false", got, want)
				}
			}
			for _, not := range tc.isNot {
				if errors.Is(got, not) {
					t.Errorf("errors.Is(%v, %v) = true, want false", got, not)
				}
			}
			if tc.wantMsg != "" && !strings.Contains(got.Error(), tc.wantMsg) {
				t.Errorf("message %q lacks %q", got.Error(), tc.wantMsg)
			}
			assertNoDriverError(t, got)
			// Mapping twice is stable.
			if again := MapError(ctx, got); again != got {
				t.Errorf("MapError is not idempotent: %v -> %v", got, again)
			}
		})
	}
}

// assertNoDriverError checks ARCH-32: the chain of a mapped error holds no driver type.
func assertNoDriverError(t *testing.T, err error) {
	t.Helper()
	type coder interface{ Code() int }
	var c coder
	if errors.As(err, &c) {
		t.Errorf("driver error %T leaks through %v", c, err)
	}
}

func TestMapErrorPassThrough(t *testing.T) {
	ctx := context.Background()
	if MapError(ctx, nil) != nil {
		t.Fatal("MapError(nil) != nil")
	}
	for _, in := range []error{context.Canceled, context.DeadlineExceeded, fmt.Errorf("wrapped: %w", context.Canceled)} {
		if got := MapError(ctx, in); got != in {
			t.Errorf("MapError(%v) = %v, want unchanged", in, got)
		}
	}
	if got := MapError(ctx, sql.ErrTxDone); !errors.Is(got, ErrTxClosed) {
		t.Errorf("MapError(ErrTxDone) = %v, want ErrTxClosed", got)
	}
}

// A statement interrupted because its context ended maps to the context error.
func TestMapErrorInterruptBecomesContextError(t *testing.T) {
	db := openTest(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := db.sql.ExecContext(ctx, "SELECT 1")
	if got := MapError(ctx, err); !errors.Is(got, context.Canceled) {
		t.Fatalf("MapError = %v, want context.Canceled", got)
	}
}

// TestSettingsVersionCAS is the reference for repositories: insert with version 1, guarded update, stale update.
func TestSettingsVersionCAS(t *testing.T) {
	db := openTest(t, nil)
	ctx := context.Background()
	q := db.Queries(ctx)

	if _, err := q.GetSettings(ctx); !errors.Is(MapError(ctx, err), shared.ErrNotFound) {
		t.Fatalf("GetSettings on an empty table: %v, want ErrNotFound", err)
	}
	if err := MapError(ctx, q.InsertSettings(ctx, insertParams(10))); err != nil {
		t.Fatalf("insert: %v", err)
	}
	stored, err := q.GetSettings(ctx)
	if err != nil || stored.Version != 1 || stored.UpdatedAt != 10 {
		t.Fatalf("stored = %+v, %v; want version 1", stored, err)
	}

	update := func(expected, updatedAt int64) error {
		rows, err := q.UpdateSettings(ctx, sqlcgen.UpdateSettingsParams{
			RecordingPromptsEnabled: 0, ProcessOnlyOnAcPower: 1, NotifyOnProcessingComplete: 1,
			UpdatedAt: updatedAt, ExpectedVersion: expected,
		})
		return VersionCAS(ctx, rows, err)
	}

	tests := []struct {
		name     string
		expected int64
		wantErr  error
		wantVer  int64
	}{
		{"loaded version wins", 1, nil, 2},
		{"same instance saves again with the new version", 2, nil, 3},
		{"stale version loses", 1, shared.ErrVersionConflict, 3},
		{"version 0 never matches a stored row", 0, shared.ErrVersionConflict, 3},
		{"future version never matches", 9, shared.ErrVersionConflict, 3},
	}
	for i, tc := range tests {
		err := update(tc.expected, int64(100+i))
		if tc.wantErr == nil && err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.wantErr != nil {
			if !errors.Is(err, tc.wantErr) || !errors.Is(err, shared.ErrConflict) || errors.Is(err, shared.ErrDuplicate) {
				t.Fatalf("%s: err = %v, want %v (and ErrConflict, not ErrDuplicate)", tc.name, err, tc.wantErr)
			}
		}
		got, err := q.GetSettings(ctx)
		if err != nil || got.Version != tc.wantVer {
			t.Fatalf("%s: version = %d, %v; want %d", tc.name, got.Version, err, tc.wantVer)
		}
	}
	if got, _ := q.GetSettings(ctx); got.UpdatedAt != 101 || got.RecordingPromptsEnabled != 0 {
		t.Errorf("a losing update changed the row: %+v", got)
	}
}

func TestVersionCASClassification(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		rows int64
		err  error
		is   error
	}{
		{"one row", 1, nil, nil},
		{"no row", 0, nil, shared.ErrVersionConflict},
		{"several rows is a bug", 2, nil, ErrDatabase},
		{"driver error wins over the count", 0, sql.ErrTxDone, ErrTxClosed},
	}
	for _, tc := range tests {
		err := VersionCAS(ctx, tc.rows, tc.err)
		if tc.is == nil {
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
			}
			continue
		}
		if !errors.Is(err, tc.is) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.is)
		}
	}
}

const insertJob = `INSERT INTO jobs (id, type, status, idempotency_key, max_attempts, next_run_at, created_at, updated_at)
	VALUES (?, 'stt', 'QUEUED', ?, 3, 0, 0, 0)`

const insertLesson = `INSERT INTO schedule_entries (id, title, starts_at, ends_at, timezone, source_uid, created_at, updated_at)
	VALUES (?, 'math', 1, 2, 'Europe/Berlin', ?, 0, 0)`

func insertModelSQL() string {
	return `INSERT INTO stt_models (id, name, model_version, file_ref, size_bytes, sha256, installed_at)
	VALUES (?, ?, ?, 'models/small.bin', 10, '` + strings.Repeat("a", 64) + `', 0)`
}

// ADR-0004(d): a version-0 insert whose id exists is a version conflict, a violation of any other uniqueness rule stays
// a duplicate.
func TestInsertDuplicateIdBecomesVersionConflict(t *testing.T) {
	ctx := context.Background()
	exec := func(query string, args ...any) func(db *DB) error {
		return func(db *DB) error { _, err := db.sql.ExecContext(ctx, query, args...); return err }
	}
	tests := []struct {
		name   string
		seed   func(db *DB) error
		insert func(db *DB) error
		wantVC bool // shared.ErrVersionConflict, otherwise shared.ErrDuplicate
	}{
		{
			name:   "settings duplicate id",
			seed:   func(db *DB) error { return db.Queries(ctx).InsertSettings(ctx, insertParams(1)) },
			insert: func(db *DB) error { return db.Queries(ctx).InsertSettings(ctx, insertParams(2)) },
			wantVC: true,
		},
		{
			name:   "jobs duplicate id, new idempotency key",
			seed:   exec(insertJob, "j1", "k1"),
			insert: exec(insertJob, "j1", "k2"),
			wantVC: true,
		},
		{
			name:   "jobs new id, duplicate idempotency key",
			seed:   exec(insertJob, "j1", "k1"),
			insert: exec(insertJob, "j2", "k1"),
		},
		{
			name:   "stt_models new id, existing name and model_version",
			seed:   exec(insertModelSQL(), "m1", "small", "1"),
			insert: exec(insertModelSQL(), "m2", "small", "1"),
		},
		{
			name:   "lessons new id, existing source_uid",
			seed:   exec(insertLesson, "l1", "uid-1"),
			insert: exec(insertLesson, "l2", "uid-1"),
		},
		{
			name:   "lessons duplicate id, new source_uid",
			seed:   exec(insertLesson, "l1", "uid-1"),
			insert: exec(insertLesson, "l1", "uid-2"),
			wantVC: true,
		},
		{
			name:   "stt_models duplicate id, new name",
			seed:   exec(insertModelSQL(), "m1", "small", "1"),
			insert: exec(insertModelSQL(), "m1", "base", "1"),
			wantVC: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openTest(t, nil)
			if err := tc.seed(db); err != nil {
				t.Fatal(err)
			}
			got := DuplicateAsVersionConflict(MapError(ctx, tc.insert(db)))
			if tc.wantVC {
				if !errors.Is(got, shared.ErrVersionConflict) || errors.Is(got, shared.ErrDuplicate) {
					t.Fatalf("err = %v, want ErrVersionConflict", got)
				}
				return
			}
			if !errors.Is(got, shared.ErrDuplicate) || errors.Is(got, shared.ErrVersionConflict) {
				t.Fatalf("err = %v, want ErrDuplicate and not ErrVersionConflict", got)
			}
		})
	}

	other := errors.New("other")
	if DuplicateAsVersionConflict(nil) != nil || DuplicateAsVersionConflict(other) != other {
		t.Fatal("DuplicateAsVersionConflict must pass through nil and unrelated errors")
	}
}

// The CAS inside a transaction: a rollback also undoes the version bump.
func TestVersionCASInsideTransaction(t *testing.T) {
	db := openTest(t, nil)
	ctx := context.Background()
	if err := db.Queries(ctx).InsertSettings(ctx, insertParams(1)); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err := db.Do(ctx, func(ctx context.Context) error {
		rows, err := db.Queries(ctx).UpdateSettings(ctx, sqlcgen.UpdateSettingsParams{UpdatedAt: 2, ExpectedVersion: 1})
		if err := VersionCAS(ctx, rows, err); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	got, err := db.Queries(ctx).GetSettings(ctx)
	if err != nil || got.Version != 1 || got.UpdatedAt != 1 {
		t.Fatalf("after rollback: %+v, %v; want the original row", got, err)
	}
}

func TestCountJobsByStatus(t *testing.T) {
	db := openTest(t, nil)
	ctx := context.Background()
	rows, err := db.Queries(ctx).CountJobsByStatus(ctx)
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("empty table: %v, %v; want an empty non-nil slice", rows, err)
	}
	for i, status := range []string{"QUEUED", "QUEUED", "RETRY_WAIT"} {
		mustExec(t, db, `INSERT INTO jobs (id, type, status, idempotency_key, max_attempts, next_run_at, created_at, updated_at)
			VALUES (?, 'stt', ?, ?, 3, 0, 0, 0)`, fmt.Sprintf("j%d", i), status, fmt.Sprintf("k%d", i))
	}
	rows, err = db.Queries(ctx).CountJobsByStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []sqlcgen.CountJobsByStatusRow{{Status: "QUEUED", Total: 2}, {Status: "RETRY_WAIT", Total: 1}}
	if len(rows) != len(want) || rows[0] != want[0] || rows[1] != want[1] {
		t.Fatalf("rows = %v, want %v", rows, want)
	}
}

// Documents what SQLite reports when one INSERT violates both the primary key and a secondary UNIQUE rule (the
// limitation described at DuplicateAsVersionConflict): the secondary rule, so the result is shared.ErrDuplicate and not
// a version conflict. If a SQLite upgrade changes the order this test fails and the documentation has to follow.
func TestUniquenessCheckOrder(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		query string
		args  []any
	}{
		{"jobs: same id and same idempotency key", insertJob, []any{"j1", "k1"}},
		{"stt_models: same id and same name+model_version", insertModelSQL(), []any{"m1", "small", "1"}},
		{"lessons: same id and same source_uid (partial unique index)", insertLesson, []any{"l1", "uid-1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openTest(t, nil)
			mustExec(t, db, tc.query, tc.args...)
			_, raw := db.sql.ExecContext(ctx, tc.query, tc.args...)
			err := MapError(ctx, raw)
			if !errors.Is(err, shared.ErrDuplicate) {
				t.Fatalf("err = %v, want a duplicate", err)
			}
			if IsPrimaryKeyViolation(err) {
				t.Errorf("SQLite now reports the primary key first: %v; update the doc of DuplicateAsVersionConflict", err)
			}
			if got := DuplicateAsVersionConflict(err); !errors.Is(got, shared.ErrDuplicate) {
				t.Errorf("DuplicateAsVersionConflict = %v, want ErrDuplicate for a double collision", got)
			}
		})
	}
}

func TestMapErrorClosedPool(t *testing.T) {
	db := openTest(t, nil)
	ctx := context.Background()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := db.Queries(ctx).GetSettings(ctx)
	if err == nil {
		t.Fatal("query on a closed pool succeeded")
	}
	if got := MapError(ctx, err); !errors.Is(got, ErrClosed) {
		t.Fatalf("MapError(%q) = %v, want ErrClosed", err, got)
	}
}

// A call whose context has ended before the driver answered BUSY reports the context error, not a busy database.
func TestMapErrorBusyAfterContextEnded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busyctx.db")
	holder := openAt(t, path, nil)
	other := openAt(t, path, func(o *Options) { o.BusyTimeout = 100 * time.Millisecond; o.Migrate = false })
	release := holdWriteLock(t, holder)
	defer release()

	raw := func() error {
		tx, err := other.sql.BeginTx(context.Background(), nil)
		if err == nil {
			_ = tx.Rollback()
		}
		return err
	}
	if got := MapError(context.Background(), raw()); !errors.Is(got, ErrBusy) {
		t.Fatalf("live context: %v, want ErrBusy", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := MapError(ctx, raw())
	if !errors.Is(got, context.Canceled) || errors.Is(got, ErrBusy) {
		t.Fatalf("ended context: %v, want context.Canceled and not ErrBusy", got)
	}
}
