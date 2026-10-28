package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
)

// purgeInterval is how often the purge of deleted assignments runs after the one at the start (ADR-0029 addendum 2).
const purgeInterval = 24 * time.Hour

const purgeStep = "deleted assignments past the restore window"

// sleeper waits on the time source of the use cases (ARCH-24).
type sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// wirePurge builds the purge of the assignments deleted longer than the restore window (TASK-274): a startup cleanup
// and a loop that runs it once a day. Its clock is the one of «Вернуть» (homeworkflow.Service), so both see the same
// window end; the files go through the Storage Manager.
func (c *core) wirePurge(e env, log *slog.Logger) error {
	purger, err := homeworkflow.NewPurger(homeworkflow.PurgeDeps{
		Assignments: sqlite.NewAssignmentRepository(c.db), Proposals: sqlite.NewProposalRepository(c.db),
		Deliveries: sqlite.NewDeliveryRepository(c.db), Drafts: sqlite.NewDraftRepository(c.db),
		Works: sqlite.NewWorkRepository(c.db), Files: c.store, Clock: e.local, Tx: c.db, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: assignment purge: %w", err)
	}
	c.cleanups = append(c.cleanups, cleanup{name: purgeStep, run: purger.PurgeDeleted})
	c.purge = loop{name: "assignment purge", run: purgeLoop(e.clock, purger.PurgeDeleted, log)}
	return nil
}

// purgeLoop runs purge every purgeInterval on sl's time until ctx ends; the startup cleanup made the first run. A
// failed run is logged and the next one tries again.
func purgeLoop(sl sleeper, purge func(ctx context.Context) (int, error), log *slog.Logger) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		for {
			if err := sl.Sleep(ctx, purgeInterval); err != nil {
				return err
			}
			n, err := purge(ctx)
			switch {
			case err != nil && ctx.Err() == nil:
				log.Error("daily cleanup failed", "step", purgeStep, "removed", n, "err", err)
			case n > 0:
				log.Info("daily cleanup", "step", purgeStep, "removed", n)
			}
		}
	}
}
