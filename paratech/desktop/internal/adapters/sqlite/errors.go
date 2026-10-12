package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	msqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"paratech/desktop/internal/domain/shared"
)

// errDBClosedText is the message of database/sql's unexported errDBClosed, returned by every call on a closed pool.
// The error value is not exported, so the text is the only handle (pinned by TestMapErrorClosedPool).
const errDBClosedText = "sql: database is closed"

// Typed errors of the adapter (ARCH-32). They can be checked with errors.Is; the domain sentinels shared.ErrDuplicate,
// shared.ErrConflict, shared.ErrVersionConflict and shared.ErrNotFound are used where the domain defines the meaning.
var (
	// ErrBusy means the database stayed locked by another writer longer than the busy timeout. Nothing was written;
	// the caller may retry later.
	ErrBusy = errors.New("database is busy")

	// ErrConstraint means a constraint other than uniqueness and foreign keys rejected the write: a CHECK or a NOT
	// NULL. It signals a mismatch between the aggregate and the schema (a bug), not a state the user can fix.
	ErrConstraint = errors.New("constraint violation")

	// ErrClosed means the DB handle was closed.
	ErrClosed = errors.New("database is closed")

	// ErrTxClosed means a context that carried a transaction was used after that transaction finished.
	ErrTxClosed = errors.New("transaction is finished")

	// ErrSchemaTooNew means the database was migrated by a newer build than this one knows.
	ErrSchemaTooNew = errors.New("database schema is newer than this build")

	// ErrDatabase is any other database failure (I/O error, corrupt file, bad SQL, ...). The message carries the
	// driver text, the driver error itself is not exposed.
	ErrDatabase = errors.New("database error")
)

// Error is a database error translated by MapError. errors.Is matches Kind (and what Kind wraps); the driver error is
// deliberately not part of the chain so that callers cannot depend on the driver (ARCH-32).
type Error struct {
	Kind   error  // one of the sentinels above or a domain sentinel
	Detail string // driver message; contains table and column names, never row values

	// primaryKey is set when Kind is shared.ErrDuplicate and the violated rule is the primary key (or the rowid) of
	// the table, as opposed to a secondary UNIQUE rule. It is what lets a repository tell "someone created this id
	// first" (shared.ErrVersionConflict, ADR-0004(d)) from "this natural key exists" (shared.ErrDuplicate). It is
	// unexported so that the driver's result codes stay out of the API; use IsPrimaryKeyViolation.
	primaryKey bool
}

// Error implements error.
func (e *Error) Error() string {
	if e.Detail == "" {
		return "sqlite: " + e.Kind.Error()
	}
	return "sqlite: " + e.Kind.Error() + ": " + e.Detail
}

// Unwrap lets errors.Is see the sentinel.
func (e *Error) Unwrap() error { return e.Kind }

// MapError translates an error returned by database/sql or the driver into a domain sentinel or a typed adapter error
// (ARCH-32). ctx is the context of the failed call: when the driver reports an interrupted statement because the
// context ended, the context error is returned instead (errors.Is(err, context.Canceled) holds).
//
//	unique / primary key violation  -> shared.ErrDuplicate (also errors.Is shared.ErrConflict); a primary key or
//	                                   rowid violation is also recognised by IsPrimaryKeyViolation
//	foreign key violation           -> shared.ErrConflict (also the RESTRICT of a parent delete)
//	CHECK / NOT NULL violation      -> ErrConstraint
//	database locked (busy timeout)  -> ErrBusy (the context error instead, when ctx has ended)
//	database handle closed          -> ErrClosed
//	no rows (sql.ErrNoRows)         -> shared.ErrNotFound
//	finished transaction            -> ErrTxClosed
//	anything else                   -> ErrDatabase
//
// nil maps to nil, context errors and errors that are already translated pass through unchanged.
func MapError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var mapped *Error
	if errors.As(err, &mapped) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		return &Error{Kind: shared.ErrNotFound}
	}
	if errors.Is(err, sql.ErrTxDone) {
		return &Error{Kind: ErrTxClosed}
	}
	if strings.Contains(err.Error(), errDBClosedText) {
		return &Error{Kind: ErrClosed}
	}
	var se *msqlite.Error
	if !errors.As(err, &se) {
		return &Error{Kind: ErrDatabase, Detail: err.Error()}
	}
	code := se.Code()
	switch code {
	case sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite3.SQLITE_CONSTRAINT_ROWID:
		return &Error{Kind: shared.ErrDuplicate, Detail: se.Error(), primaryKey: true}
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE:
		return &Error{Kind: shared.ErrDuplicate, Detail: se.Error()}
	case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
		return &Error{Kind: shared.ErrConflict, Detail: se.Error()}
	case sqlite3.SQLITE_CONSTRAINT_TRIGGER:
		// Verified: deleting a parent row that is referenced with ON DELETE RESTRICT (the default STT model of
		// the settings, ADR-0004(g)) is reported with the extended code CONSTRAINT_TRIGGER and the message of a
		// foreign key failure, not with CONSTRAINT_FOREIGNKEY. Both are the same conflict for the caller.
		if strings.Contains(se.Error(), "FOREIGN KEY constraint failed") {
			return &Error{Kind: shared.ErrConflict, Detail: se.Error()}
		}
	}
	switch code & 0xff { // the primary result code is in the low byte of an extended code
	case sqlite3.SQLITE_CONSTRAINT:
		return &Error{Kind: ErrConstraint, Detail: se.Error()}
	case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED, sqlite3.SQLITE_INTERRUPT:
		// The driver interrupts a statement when the context of the call ends, but SQLite's busy handler does not
		// notice the interrupt, so a call whose context ended while it waited for a lock fails with BUSY after the
		// busy timeout. Either way the caller gave up first: report that, not a database that is "busy".
		if ctx != nil && ctx.Err() != nil {
			return fmt.Errorf("sqlite: %w", ctx.Err())
		}
		if code&0xff != sqlite3.SQLITE_INTERRUPT {
			return &Error{Kind: ErrBusy, Detail: se.Error()}
		}
	}
	return &Error{Kind: ErrDatabase, Detail: se.Error()}
}

// VersionCAS finishes a version-guarded write (ADR-0004(d)): rows is the number of rows affected by an
// "UPDATE/DELETE ... WHERE id = ? AND version = ?" and err its error. A database error is translated with MapError;
// zero rows affected means the row changed or was removed since it was loaded and is reported as
// shared.ErrVersionConflict; exactly one row is success. Repositories use it for every Save of a stored aggregate.
func VersionCAS(ctx context.Context, rows int64, err error) error {
	if err != nil {
		return MapError(ctx, err)
	}
	switch {
	case rows == 0:
		return shared.ErrVersionConflict
	case rows > 1:
		return &Error{Kind: ErrDatabase, Detail: fmt.Sprintf("version-guarded write changed %d rows", rows)}
	}
	return nil
}

// IsPrimaryKeyViolation reports whether err (as returned by MapError) is a uniqueness violation of the primary key or
// the rowid of a table, not of a secondary UNIQUE rule.
func IsPrimaryKeyViolation(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.primaryKey
}

// DuplicateAsVersionConflict is for the INSERT of a never-stored aggregate (version 0): when the primary key already
// exists, someone else created the row first and ADR-0004(d) wants shared.ErrVersionConflict instead of the generic
// shared.ErrDuplicate that MapError returns. Only a primary key (or rowid) violation is converted. A violation of a
// secondary uniqueness rule (the delivery idempotency key, stt_models (name, model_version), the source_uid of a
// lesson) and every other error, and nil, pass through, so the use case still sees shared.ErrDuplicate for those.
//
// Limitation: an INSERT that violates both the primary key and a secondary rule reports the secondary rule, because
// SQLite checks the unique indexes of a table in an order in which the primary key comes last (observed for jobs,
// stt_models and schedule_entries by TestUniquenessCheckOrder, not documented by SQLite). The result is then
// shared.ErrDuplicate, which is also the right answer for the cases that exist: the same job, model or delivery
// stored twice. A repository that must tell the two apart in a double collision loads the row by id.
func DuplicateAsVersionConflict(err error) error {
	if IsPrimaryKeyViolation(err) {
		return shared.ErrVersionConflict
	}
	return err
}
