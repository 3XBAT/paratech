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
//   - Writers of this process take turns: Do first takes the write slot (waiting for it ends as soon as ctx ends) and
//     only then a pool connection, so a writer that waits holds no connection and plain reads always find one. The
//     slot is released after commit or rollback, also when fn panics or calls runtime.Goexit. A Do started inside fn
//     with a context that does NOT carry the transaction (not the ctx passed to fn) waits for the outer Do and, with a
//     context that never ends, blocks forever: always pass the context given to fn on.
//   - The transaction starts with BEGIN IMMEDIATE (the DSN sets _txlock=immediate), so the write lock is taken up
//     front and a read inside fn cannot be invalidated by another writer. Thanks to the slot BEGIN contends only with
//     other PROCESSES using the file; it then waits for the busy timeout, and if the lock is not available in time the
//     result is ErrBusy.
//   - The context passed to fn carries the transaction (DBTX, Queries). A Do called with that context joins the outer
//     transaction without a savepoint and without the slot; only the outermost Do commits or rolls back, and an error
//     of a nested fn that the outer fn swallows does not undo the nested writes.
//   - fn returning nil commits. An error, a panic (which continues after the rollback) or runtime.Goexit rolls back.
//     The error of fn is returned unchanged; errors of begin and commit are translated with MapError.
//   - Cancelling ctx interrupts the running statement and rolls the transaction back (database/sql); a commit after
//     cancellation fails with the context error. Waiting for the write slot and for the lock of another process is
//     also left as soon as ctx ends (see begin).
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
	// A context that has ended must not start anything, even if the slot is free (select would pick at random).
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := db.lockWriter(ctx); err != nil {
		return err
	}

	// On error begin has released the slot, or handed it to the background attempt that releases it when BEGIN ends.
	tx, err := db.begin(ctx, db.unlockWriter)
	if err != nil {
		return MapError(ctx, err)
	}
	defer db.unlockWriter() // registered before the rollback below, so it runs after it
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
		// A cancelled ctx makes database/sql roll back on its own, so Commit may report ErrTxDone.
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, sql.ErrTxDone) {
			return ctxErr
		}

		return MapError(ctx, err)
	}
	committed = true
	return nil
}

// lockWriter takes the write slot, giving up when ctx ends or the DB is closed.
func (db *DB) lockWriter(ctx context.Context) error {
	select {
	case db.writeSlot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-db.done:
		return &Error{Kind: ErrClosed}
	}
	if db.closed.Load() { // the slot and the close may have been ready together
		db.unlockWriter()
		return &Error{Kind: ErrClosed}
	}
	return nil
}

func (db *DB) unlockWriter() { <-db.writeSlot }

// beginResult is the outcome of a BEGIN that ran in the background.
type beginResult struct {
	tx  *sql.Tx
	err error
}

// Handoff states of a background BEGIN between the waiting caller and the goroutine.
const (
	beginPending   int32 = iota
	beginDelivered       // the goroutine finished first and the caller takes the result
	beginAbandoned       // the caller left (context ended) and the goroutine cleans up
)

// begin starts the write transaction while the caller holds the write slot. release gives the slot back; begin
// guarantees it is called exactly once when begin returns an error, and never when it returns a transaction (the
// caller releases it after commit or rollback).
//
// SQLite's busy handler sleeps and does not look at sqlite3_interrupt (TestInterruptDoesNotStopBusyHandler), so a
// context that ends while BEGIN IMMEDIATE waits for a lock of another process would otherwise be noticed only when
// the busy timeout expires. When ctx can end, BEGIN runs in a goroutine and begin returns the context error at once.
// The abandoned attempt keeps the slot (and its one connection) until BEGIN returns, which is at most the busy timeout
// later; then it rolls back a transaction that was obtained just after the cancellation, releases the slot and ends.
// Other writers therefore never see more than one abandoned attempt, and Close waits for it (DB.bg).
func (db *DB) begin(ctx context.Context, release func()) (*sql.Tx, error) {
	if ctx.Done() == nil {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			release()
		}
		return tx, err
	}
	db.mu.RLock()
	if db.closed.Load() {
		db.mu.RUnlock()
		release()
		return nil, &Error{Kind: ErrClosed}
	}
	db.bg.Add(1)
	db.mu.RUnlock()

	var state atomic.Int32
	ch := make(chan beginResult, 1)
	go func() {
		defer db.bg.Done()
		tx, err := db.sql.BeginTx(ctx, nil)
		if state.CompareAndSwap(beginPending, beginDelivered) {
			ch <- beginResult{tx, err}
			return
		}
		// The caller left. BeginTx may still have succeeded if the lock came just before the cancellation.
		if tx != nil {
			_ = tx.Rollback()
		}
		release()
	}()

	select {
	case r := <-ch:
		return db.finishBegin(r, release)
	case <-ctx.Done():
		if state.CompareAndSwap(beginPending, beginAbandoned) {
			return nil, ctx.Err()
		}
		return db.finishBegin(<-ch, release) // the goroutine was just faster: the result is already there
	}
}

func (db *DB) finishBegin(r beginResult, release func()) (*sql.Tx, error) {
	if r.err != nil {
		release()
		return nil, r.err
	}
	return r.tx, nil
}
