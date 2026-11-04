package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/agentretention"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/subjectpurge"
)

// purgeInterval is how often the purges run after the one at the start: the deleted assignments (ADR-0029 addendum 2),
// the removed subjects (ADR-0027) and the details of old agent requests (ADR-0028).
const purgeInterval = 24 * time.Hour

// The names of the purges as startup cleanup steps and in the log.
const (
	purgeStep          = "deleted assignments past the restore window"
	subjectPurgeStep   = "removed subjects past the restore window"
	agentRetentionStep = "agent history details past 90 days"
)

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
	c.purge = loop{name: "assignment purge", run: purgeLoop(e.clock, purgeStep, purger.PurgeDeleted, log)}
	return nil
}

// wireSubjectPurge builds the purge of the subjects removed longer than the restore window (TASK-308, ADR-0027): a
// startup cleanup and a loop that runs it once a day, like the assignment purge. Its clock is the one of «Вернуть»
// (library.Service); the files go through the Storage Manager. The library gets it too, to free the name of an
// expired subject at once.
func (c *core) wireSubjectPurge(e env, notes *sqlite.NoteRepository, log *slog.Logger) (*subjectpurge.Purger, error) {
	purger, err := subjectpurge.New(subjectpurge.Deps{
		Subjects: sqlite.NewSubjectRepository(c.db), Notes: notes, TicketSets: sqlite.NewSubjectTicketSets(c.db),
		Materials: sqlite.NewMaterialRepository(c.db), Blobs: c.store, Clock: e.local, Log: log,
	})
	if err != nil {
		return nil, fmt.Errorf("app: subject purge: %w", err)
	}
	c.cleanups = append(c.cleanups, cleanup{name: subjectPurgeStep, run: purger.Purge})
	c.subjectPurge = loop{name: "subject purge", run: purgeLoop(e.clock, subjectPurgeStep, purger.Purge, log)}
	return purger, nil
}

// wireAgentRetention builds the purge of the technical details of agent requests that ended 90 days ago (TASK-250,
// ADR-0028 «Retention»): a startup cleanup after the recovery of the agent requests and a loop once a day.
func (c *core) wireAgentRetention(e env, log *slog.Logger) error {
	purger, err := agentretention.New(agentretention.Deps{
		Requests: sqlite.NewAgentRetention(c.db), Tx: c.db, Clock: e.clock, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: agent history retention: %w", err)
	}
	c.cleanups = append(c.cleanups, cleanup{name: agentRetentionStep, run: purger.Purge})
	c.agentRetention = loop{name: "agent history retention", run: purgeLoop(e.clock, agentRetentionStep, purger.Purge, log)}
	return nil
}

// purgeLoop runs purge every purgeInterval on sl's time until ctx ends; the startup cleanup made the first run. A
// failed run is logged with step and the next one tries again.
func purgeLoop(sl sleeper, step string, purge func(ctx context.Context) (int, error), log *slog.Logger) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		for {
			if err := sl.Sleep(ctx, purgeInterval); err != nil {
				return err
			}
			n, err := purge(ctx)
			switch {
			case err != nil && ctx.Err() == nil:
				log.Error("daily cleanup failed", "step", step, "removed", n, "err", err)
			case n > 0:
				log.Info("daily cleanup", "step", step, "removed", n)
			}
		}
	}
}
