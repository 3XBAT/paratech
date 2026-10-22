package app

import (
	"context"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/senddelivery"
	agentloop "paratech/desktop/internal/input/agent"
)

// agentReads are the use cases the agent reads through: the library and the knowledge base, the recordings and jobs,
// the assignments and their deliveries.
type agentReads struct {
	library     *library.Service
	knowledge   *knowledgebase.Service
	activity    agentloop.Activity
	assignments *homeworkflow.Service
	deliveries  *senddelivery.Service
}

// wireAgent builds the agent core (TASK-116, TASK-117, ADR-0021): the steps of a request on the Hub's AgentStep, the
// action journal and the Driver that runs the read tools. The panel comes with TASK-119; until then the Driver's
// updates go nowhere and nothing calls it. Today and the zone of the base come from the student's clock.
func (c *core) wireAgent(e env, model agentturn.AgentModel, reads agentReads, log *slog.Logger) error {
	facts, err := agentloop.NewFacts(reads.knowledge)
	if err != nil {
		return fmt.Errorf("app: agent: %w", err)
	}
	subjects, err := agentloop.NewBaseSubjects(reads.library)
	if err != nil {
		return fmt.Errorf("app: agent: %w", err)
	}
	c.agent, err = agentturn.New(agentturn.Deps{
		Model: model, Threads: sqlite.NewAgentThreadRepository(c.db), History: sqlite.NewAgentHistory(c.db, e.clock),
		Subjects: subjects, Facts: facts, Tx: c.db, Clock: e.local, IDs: e.ids, Log: log,
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
	c.driver, err = agentloop.New(agentloop.Deps{
		Turns: c.agent, Sink: noPanel{}, Library: reads.library, Knowledge: reads.knowledge, Activity: reads.activity,
		Assignments: reads.assignments, Deliveries: reads.deliveries, Clock: e.local, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: agent driver: %w", err)
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

// noPanel drops the Driver's updates until the panel binding exists (TASK-119).
type noPanel struct{}

func (noPanel) Publish(agentloop.Update) {}
