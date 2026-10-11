package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
)

// txKey identifies the transaction in a context. It carries the DB so that two handles never mix transactions.
type txKey struct{ db *DB }

// activeTx is the transaction that travels in the context passed to the function of Do.
type activeTx struct {
	tx   *sql.Tx
	done atomic.Bool // set before commit/rollback; a context that outlives the transaction must not join it
}

func (db *DB) activeTx(ctx context.Context) *activeTx {
	st, _ := ctx.Value(txKey{db}).(*activeTx)
	return st
}

// InTx reports whether ctx carries a running transaction of this database.
func (db *DB) InTx(ctx context.Context) bool {
	st := db.activeTx(ctx)
	return st != nil && !st.done.Load()
}

// DBTX returns what a query must run on: the transaction carried by ctx, otherwise the connection pool. A repository
// called with the context of a running Do therefore takes part in that transaction; called with another context it
// does not (use case contract, see the Tx port).
func (db *DB) DBTX(ctx context.Context) sqlcgen.DBTX {
	if st := db.activeTx(ctx); st != nil {
		return st.tx
	}
	return db.sql
}

// Do implements the Tx port of the use cases (ARCH-23, ADR-0004): it runs fn in one write transaction.
//
//   - The transaction starts with BEGIN IMMEDIATE (the DSN sets _txlock=immediate), so the write lock is taken up
//     front, a read inside fn cannot be invalidated by another writer, and a second writer waits for the busy timeout
//     instead of failing on a lock upgrade. If the lock is not available in time the result is ErrBusy.
//   - The context passed to fn carries the transaction (DBTX, Queries). A Do called with that context joins the outer
//     transaction without a savepoint; only the outermost Do commits or rolls back, and an error of a nested fn that
//     the outer fn swallows does not undo the nested writes.
//   - fn returning nil commits. An error, a panic (which continues after the rollback) or runtime.Goexit rolls back.
//     The error of fn is returned unchanged; errors of begin and commit are translated with MapError.
//   - Cancelling ctx interrupts the running statement and rolls the transaction back (database/sql); a commit after
//     cancellation fails with the context error. Waiting for the write lock is also left as soon as ctx ends (see
//     begin).
//   - A closed DB gives ErrClosed without calling fn.
func (db *DB) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if fn == nil {
		return errors.New("sqlite: Do called with a nil function")
	}
	if st := db.activeTx(ctx); st != nil {
		if st.done.Load() {
			return &Error{Kind: ErrTxClosed}
		}
		return fn(ctx)
	}
	if db.closed.Load() {
		return &Error{Kind: ErrClosed}
	}

	tx, err := db.begin(ctx)
	if err != nil {
		return MapError(ctx, err)
	}
	st := &activeTx{tx: tx}
	committed := false
	defer func() {
		st.done.Store(true)
		if !committed {
			// Error, panic or Goexit. After a context cancellation database/sql has already rolled back and
			// returns ErrTxDone, which is not interesting here.
			_ = tx.Rollback()
		}
	}()

	if err := fn(context.WithValue(ctx, txKey{db}, st)); err != nil {
		return err
	}
	st.done.Store(true)
	if err := tx.Commit(); err != nil {
		return MapError(ctx, err)
	}
	committed = true
	return nil
}

// begin starts the write transaction. SQLite's busy handler sleeps and does not look at sqlite3_interrupt, so a
// context that ends while BEGIN IMMEDIATE waits for the lock would otherwise be noticed only when the busy timeout
// expires (verified by TestBeginImmediateWaitIsInterruptedByContext). When ctx can end, BEGIN runs in a goroutine and
// begin returns the context error at once. database/sql rolls a transaction whose context ended back by itself, so
// the abandoned attempt releases its connection when the busy timeout expires at the latest; the goroutine rolls back
// explicitly as well in case the lock was obtained just before the cancellation.
func (db *DB) begin(ctx context.Context) (*sql.Tx, error) {
	if ctx.Done() == nil {
		return db.sql.BeginTx(ctx, nil)
	}
	type result struct {
		tx  *sql.Tx
		err error
	}
	ch := make(chan result, 1)
	go func() {
		tx, err := db.sql.BeginTx(ctx, nil)
		ch <- result{tx, err}
	}()
	select {
	case r := <-ch:
		return r.tx, r.err
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.tx != nil {
				_ = r.tx.Rollback()
			}
		}()
		return nil, ctx.Err()
	}
}
