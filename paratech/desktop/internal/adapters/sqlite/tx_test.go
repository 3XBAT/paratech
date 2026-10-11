package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// insertSetting writes the settings row through the sqlc queries bound to ctx.
func insertSetting(ctx context.Context, db *DB, updatedAt int64) error {
	return MapError(ctx, db.Queries(ctx).InsertSettings(ctx, insertParams(updatedAt)))
}

func settingsCount(t *testing.T, db *DB) int64 {
	t.Helper()
	return scanInt(t, db.sql, "SELECT count(*) FROM settings")
}

func TestDoCommitsOnNil(t *testing.T) {
	db := openTest(t, nil)
	ctx := context.Background()
	err := db.Do(ctx, func(ctx context.Context) error {
		if !db.InTx(ctx) {
			t.Error("InTx is false inside Do")
		}
		return insertSetting(ctx, db, 1)
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if db.InTx(ctx) {
		t.Error("InTx is true for the outer context")
	}
	if n := settingsCount(t, db); n != 1 {
		t.Fatalf("settings rows = %d, want 1", n)
	}
}

func TestDoRollsBackOnError(t *testing.T) {
	db := openTest(t, nil)
	sentinel := errors.New("business failure")
	err := db.Do(context.Background(), func(ctx context.Context) error {
		if err := insertSetting(ctx, db, 1); err != nil {
			return err
		}
		return fmt.Errorf("step failed: %w", sentinel)
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Do error = %v, want the error of fn", err)
	}
	if n := settingsCount(t, db); n != 0 {
		t.Fatalf("settings rows = %d after rollback, want 0", n)
	}
}

func TestDoRollsBackOnPanicAndRepanics(t *testing.T) {
	db := openTest(t, nil)
	func() {
		defer func() {
			if r := recover(); r != "boom" {
				t.Errorf("recovered %v, want the original panic value", r)
			}
		}()
		_ = db.Do(context.Background(), func(ctx context.Context) error {
			if err := insertSetting(ctx, db, 1); err != nil {
				t.Error(err)
			}
			panic("boom")
		})
		t.Error("Do returned after a panic in fn")
	}()
	if n := settingsCount(t, db); n != 0 {
		t.Fatalf("settings rows = %d after panic, want 0", n)
	}
	// The write lock was released: a new transaction is not blocked.
	if err := db.Do(context.Background(), func(ctx context.Context) error { return insertSetting(ctx, db, 2) }); err != nil {
		t.Fatalf("Do after panic: %v", err)
	}
}

func TestDoNestedJoinsOuterTransaction(t *testing.T) {
	ctx := context.Background()

	t.Run("outer error rolls back nested writes", func(t *testing.T) {
		db := openTest(t, nil)
		errOuter := errors.New("outer failed")
		err := db.Do(ctx, func(ctx context.Context) error {
			if err := db.Do(ctx, func(ctx context.Context) error { return insertSetting(ctx, db, 1) }); err != nil {
				return err
			}
			// The nested write is visible inside the transaction, invisible to other connections until commit.
			if n := scanInt(t, db.DBTX(ctx), "SELECT count(*) FROM settings"); n != 1 {
				t.Errorf("rows inside the transaction = %d, want 1", n)
			}
			if n := settingsCount(t, db); n != 0 {
				t.Errorf("rows seen from another connection before commit = %d, want 0", n)
			}
			return errOuter
		})
		if !errors.Is(err, errOuter) {
			t.Fatalf("Do error = %v", err)
		}
		if n := settingsCount(t, db); n != 0 {
			t.Fatalf("rows after outer rollback = %d, want 0", n)
		}
	})

	t.Run("nested Do shares the same transaction", func(t *testing.T) {
		db := openTest(t, nil)
		var outerTx, innerTx any
		err := db.Do(ctx, func(ctx context.Context) error {
			outerTx = db.DBTX(ctx)
			return db.Do(ctx, func(ctx context.Context) error {
				innerTx = db.DBTX(ctx)
				return nil
			})
		})
		if err != nil {
			t.Fatal(err)
		}
		if outerTx != innerTx {
			t.Fatal("nested Do did not join the outer transaction")
		}
		if outerTx == any(db.sql) {
			t.Fatal("DBTX inside Do returned the pool")
		}
	})

	t.Run("nested error is returned and the outer decides", func(t *testing.T) {
		db := openTest(t, nil)
		errInner := errors.New("inner failed")
		err := db.Do(ctx, func(ctx context.Context) error {
			if err := insertSetting(ctx, db, 1); err != nil {
				return err
			}
			if err := db.Do(ctx, func(context.Context) error { return errInner }); !errors.Is(err, errInner) {
				t.Errorf("nested Do error = %v, want %v", err, errInner)
			}
			return nil // the outer swallows it: the whole transaction commits (no savepoints, see Tx port)
		})
		if err != nil {
			t.Fatal(err)
		}
		if n := settingsCount(t, db); n != 1 {
			t.Fatalf("rows = %d, want 1", n)
		}
	})

	t.Run("a finished transaction cannot be joined", func(t *testing.T) {
		db := openTest(t, nil)
		var stale context.Context
		if err := db.Do(ctx, func(ctx context.Context) error { stale = ctx; return nil }); err != nil {
			t.Fatal(err)
		}
		called := false
		err := db.Do(stale, func(context.Context) error { called = true; return nil })
		if !errors.Is(err, ErrTxClosed) || called {
			t.Fatalf("Do on a stale context: err %v, fn called %v; want ErrTxClosed and no call", err, called)
		}
		if db.InTx(stale) {
			t.Error("InTx is true for a finished transaction")
		}
		err = insertSetting(stale, db, 1)
		if !errors.Is(err, ErrTxClosed) {
			t.Fatalf("query on a finished transaction: %v, want ErrTxClosed", err)
		}
	})
}

func TestDoHonorsContextCancellation(t *testing.T) {
	t.Run("cancelled before begin", func(t *testing.T) {
		db := openTest(t, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		called := false
		err := db.Do(ctx, func(context.Context) error { called = true; return nil })
		if !errors.Is(err, context.Canceled) || called {
			t.Fatalf("err %v, fn called %v; want context.Canceled and no call", err, called)
		}
	})

	t.Run("cancelled inside fn, fn returns nil", func(t *testing.T) {
		db := openTest(t, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := db.Do(ctx, func(ctx context.Context) error {
			if err := insertSetting(ctx, db, 1); err != nil {
				return err
			}
			cancel()
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if n := settingsCount(t, db); n != 0 {
			t.Fatalf("rows = %d, want 0: a cancelled transaction must not commit", n)
		}
	})

	t.Run("statement after cancellation fails with the context error", func(t *testing.T) {
		db := openTest(t, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := db.Do(ctx, func(ctx context.Context) error {
			cancel()
			return insertSetting(ctx, db, 1)
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if n := settingsCount(t, db); n != 0 {
			t.Fatalf("rows = %d, want 0", n)
		}
	})
}

func TestDoOnClosedDB(t *testing.T) {
	db := openTest(t, nil)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	called := false
	err := db.Do(context.Background(), func(context.Context) error { called = true; return nil })
	if !errors.Is(err, ErrClosed) || called {
		t.Fatalf("err %v, fn called %v; want ErrClosed and no call", err, called)
	}
	if err := db.Migrate(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Migrate on a closed DB: %v, want ErrClosed", err)
	}
}

func TestDoRejectsNilFunction(t *testing.T) {
	db := openTest(t, nil)
	if err := db.Do(context.Background(), nil); err == nil {
		t.Fatal("Do(nil) succeeded")
	}
}

// ---- BEGIN IMMEDIATE and writer serialization -----------------------------------------------------------------

// holdWriteLock starts a transaction on db that has written nothing, and keeps it open until release is called.
// With BEGIN IMMEDIATE the write lock is taken at once; a deferred transaction would not block anybody yet.
func holdWriteLock(t *testing.T, db *DB) (release func()) {
	t.Helper()
	locked := make(chan struct{})
	free := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- db.Do(context.Background(), func(context.Context) error {
			close(locked)
			<-free
			return nil
		})
	}()
	select {
	case <-locked:
	case <-time.After(10 * time.Second):
		t.Fatal("transaction did not start")
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			close(free)
			if err := <-done; err != nil {
				t.Errorf("lock holder: %v", err)
			}
		})
	}
	t.Cleanup(release)
	return release
}

// A second handle on the same file stands for another process.
func TestBeginImmediateSecondWriterGetsBusyAfterTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.db")
	holder := openAt(t, path, nil)
	other := openAt(t, path, func(o *Options) { o.BusyTimeout = 300 * time.Millisecond; o.Migrate = false })

	release := holdWriteLock(t, holder)
	defer release()

	called := false
	start := time.Now()
	err := other.Do(context.Background(), func(context.Context) error { called = true; return nil })
	elapsed := time.Since(start)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if called {
		t.Error("fn ran although the write lock was not obtained")
	}
	if elapsed < 250*time.Millisecond || elapsed > 5*time.Second {
		t.Errorf("gave up after %v, want about the busy timeout of 300ms", elapsed)
	}
}

func TestBeginImmediateSecondWriterWaitsForRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wait.db")
	holder := openAt(t, path, nil)
	other := openAt(t, path, func(o *Options) { o.BusyTimeout = 10 * time.Second; o.Migrate = false })

	release := holdWriteLock(t, holder)
	time.AfterFunc(300*time.Millisecond, release)

	start := time.Now()
	err := other.Do(context.Background(), func(ctx context.Context) error { return insertSetting(ctx, other, 1) })
	if err != nil {
		t.Fatalf("second writer: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("second writer did not wait: %v", elapsed)
	}
	if n := settingsCount(t, other); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
}

// What Do promises: it leaves the wait for a lock of another process as soon as the context ends, although SQLite
// itself keeps waiting (see TestInterruptDoesNotStopBusyHandler).
func TestDoReturnsEarlyWhenContextEndsDuringCrossProcessWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctxwait.db")
	holder := openAt(t, path, nil)
	other := openAt(t, path, func(o *Options) { o.BusyTimeout = 30 * time.Second; o.Migrate = false })

	release := holdWriteLock(t, holder)
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := other.Do(ctx, func(context.Context) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("waited %v although the context ended after 200ms (busy timeout 30s)", elapsed)
	}
}

// Pins the claim behind begin: with the plain driver a BEGIN IMMEDIATE that waits for a lock keeps waiting until the
// busy timeout although its context ended (sqlite3_interrupt does not stop the busy handler). If a driver upgrade
// fixes this, the goroutine in begin becomes unnecessary and this test fails to say so.
func TestInterruptDoesNotStopBusyHandler(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interrupt.db")
	holder := openAt(t, path, nil)
	const busy = 1500 * time.Millisecond
	other := openAt(t, path, func(o *Options) { o.BusyTimeout = busy; o.Migrate = false })

	release := holdWriteLock(t, holder)
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	tx, err := other.sql.BeginTx(ctx, nil) // direct: without the goroutine of DB.begin
	elapsed := time.Since(start)
	if err == nil {
		_ = tx.Rollback()
		t.Fatal("BeginTx succeeded although the write lock is held")
	}
	if elapsed < busy*8/10 {
		t.Errorf("BeginTx returned after %v, the busy handler should keep waiting about %v: the driver now honours the context, simplify DB.begin", elapsed, busy)
	}
	if got := MapError(ctx, err); !errors.Is(got, context.DeadlineExceeded) {
		t.Errorf("MapError = %v, want the context error because the context had ended", got)
	}
}

// Readers are not blocked by a writer in WAL mode.
func TestReadsAreNotBlockedByWriter(t *testing.T) {
	db := openTest(t, nil)
	release := holdWriteLock(t, db)
	defer release()
	if n := settingsCount(t, db); n != 0 {
		t.Fatalf("rows = %d, want 0", n)
	}
}

func TestConcurrentWritersAreSerialized(t *testing.T) {
	db := openTest(t, nil)
	mustExec(t, db, "CREATE TABLE counter (n INTEGER NOT NULL)")
	mustExec(t, db, "INSERT INTO counter VALUES (0)")

	const workers, perWorker = 8, 15
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				err := db.Do(context.Background(), func(ctx context.Context) error {
					var n int64
					if err := db.DBTX(ctx).QueryRowContext(ctx, "SELECT n FROM counter").Scan(&n); err != nil {
						return MapError(ctx, err)
					}
					// A read-modify-write: lost updates would show if two writers overlapped.
					_, err := db.DBTX(ctx).ExecContext(ctx, "UPDATE counter SET n = ?", n+1)
					return MapError(ctx, err)
				})
				if err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("writer failed: %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT n FROM counter"); n != workers*perWorker {
		t.Fatalf("counter = %d, want %d", n, workers*perWorker)
	}
}

// ---- write slot: waiting writers hold no connection ---------------------------------------------------------------

func TestCancelledWritersLeaveNoConnectionBehind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "abandon.db")
	holder := openAt(t, path, nil)
	const busy = 800 * time.Millisecond
	other := openAt(t, path, func(o *Options) { o.BusyTimeout = busy; o.Migrate = false; o.MaxOpenConns = 4 })

	release := holdWriteLock(t, holder) // stands for another process

	const writers = 6
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			errs <- other.Do(ctx, func(context.Context) error { t.Error("fn ran"); return nil })
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Do = %v, want context.DeadlineExceeded", err)
		}
	}
	// Only the one attempt that owns the slot may still hold a connection; the waiting writers never took one.
	if in := other.sql.Stats().InUse; in > 1 {
		t.Errorf("InUse = %d right after the cancelled calls, want at most 1", in)
	}
	// Reads are not stalled by the abandoned attempt.
	start := time.Now()
	if n := settingsCount(t, other); n != 0 {
		t.Errorf("rows = %d", n)
	}
	if d := time.Since(start); d > busy/2 {
		t.Errorf("read took %v behind abandoned writers", d)
	}

	release()
	deadline := time.Now().Add(10 * time.Second)
	for other.sql.Stats().InUse != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("InUse = %d long after the busy timeout, connections leaked", other.sql.Stats().InUse)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The slot is free again.
	if err := other.Do(context.Background(), func(ctx context.Context) error { return insertSetting(ctx, other, 1) }); err != nil {
		t.Fatalf("Do after the abandoned attempts: %v", err)
	}
}

func TestReadsAreNotStalledByWaitingWriters(t *testing.T) {
	db := openTest(t, func(o *Options) { o.MaxOpenConns = 2; o.BusyTimeout = 30 * time.Second })
	ctx := context.Background()

	locked := make(chan struct{})
	free := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- db.Do(ctx, func(context.Context) error { close(locked); <-free; return nil })
	}()
	<-locked

	const waiting = 5
	var finished sync.WaitGroup
	for i := 0; i < waiting; i++ {
		finished.Add(1)
		go func() {
			defer finished.Done()
			if err := db.Do(ctx, func(context.Context) error { return nil }); err != nil {
				t.Errorf("waiting writer: %v", err)
			}
		}()
	}
	time.Sleep(200 * time.Millisecond) // let them reach the slot

	start := time.Now()
	if n := settingsCount(t, db); n != 0 {
		t.Errorf("rows = %d", n)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("read took %v while writers wait", d)
	}
	if in := db.sql.Stats().InUse; in != 1 {
		t.Errorf("InUse = %d with one running and %d waiting writers, want 1 (only the running one holds a connection)", in, waiting)
	}

	close(free)
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}
	finished.Wait()
}

func TestWaitForWriteSlotHonorsContext(t *testing.T) {
	db := openTest(t, nil)
	release := holdWriteLock(t, db)
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := db.Do(ctx, func(context.Context) error { t.Error("fn ran"); return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("waited %v for the slot", d)
	}
}

func TestCloseWakesWritersWaitingForSlot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closewait.db")
	db, err := Open(context.Background(), Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	locked := make(chan struct{})
	free := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- db.Do(context.Background(), func(context.Context) error { close(locked); <-free; return nil })
	}()
	<-locked
	waiter := make(chan error, 1)
	go func() { waiter <- db.Do(context.Background(), func(context.Context) error { return nil }) }()
	time.Sleep(50 * time.Millisecond)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waiter:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("waiting writer: %v, want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a writer waiting for the slot was not released by Close")
	}
	close(free)
	<-holderDone
}

// Close must not return while an abandoned BEGIN still runs: it holds a connection, and the files stay open.
func TestCloseWaitsForAbandonedBegin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closebegin.db")
	holder := openAt(t, path, nil)
	const busy = time.Second
	other, err := Open(context.Background(), Options{Path: path, BusyTimeout: busy})
	if err != nil {
		t.Fatal(err)
	}
	release := holdWriteLock(t, holder)
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := other.Do(ctx, func(context.Context) error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Do = %v, want context.DeadlineExceeded", err)
	}
	start := time.Now()
	if err := other.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if d := time.Since(start); d < busy/2 {
		t.Errorf("Close returned after %v, before the abandoned BEGIN could end (busy timeout %v)", d, busy)
	}
	if in := other.sql.Stats().OpenConnections; in != 0 {
		t.Errorf("%d connections open after Close", in)
	}
}
