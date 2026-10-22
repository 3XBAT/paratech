package app

import (
	"context"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/values"
)

// wireAgent builds the agent core (TASK-116, ADR-0021): the steps of a request on the Hub's AgentStep and the action
// journal, both on the agent tables. The panel and the Driver that runs the tools come with later tasks; until then
// nothing calls them except the startup recovery. Today and the zone of the base come from the student's clock.
func (c *core) wireAgent(e env, model agentturn.AgentModel, log *slog.Logger) error {
	var err error
	c.agent, err = agentturn.New(agentturn.Deps{
		Model: model, Threads: sqlite.NewAgentThreadRepository(c.db), History: sqlite.NewAgentHistory(c.db, e.clock),
		Subjects: noSubjects{}, Facts: agentturn.UnknownFacts{}, Tx: c.db, Clock: e.local, IDs: e.ids, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: agent: %w", err)
	}
	c.journal, err = agentjournal.New(agentjournal.Deps{
		Actions: sqlite.NewAgentActionRepository(c.db), Clock: e.clock, IDs: e.ids, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: agent journal: %w", err)
	}
	// A request left running stops as «нет связи»; a journal entry left PENDING becomes UNKNOWN.
	c.cleanups = append(c.cleanups,
		cleanup{name: "agent requests left running", run: c.agent.Recover},
		cleanup{name: "agent journal", run: func(ctx context.Context) (int, error) {
			rep, err := c.journal.Recover(ctx)
			return rep.Unknown + rep.Undone + rep.Refused, err
		}})
	return nil
}

// noSubjects is the subject list of the agent base until the library of subjects exists (TASK-121).
type noSubjects struct{}

func (noSubjects) BaseSubjects(context.Context) ([]values.BaseSubject, error) { return nil, nil }
