package app

import (
	"context"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/agentchats"
	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/shared"
	agentloop "paratech/desktop/internal/input/agent"
	"paratech/desktop/internal/input/wails"
)

// agentReads are the use cases the agent works through: the library and the knowledge base, the recordings and jobs,
// the schedule, the assignments and their deliveries, and for the actions the reruns of post-processing and the drafts.
type agentReads struct {
	library     *library.Service
	knowledge   *knowledgebase.Service
	activity    agentloop.Activity
	schedule    *schedulesync.Service // the lessons of the schedule find_lessons lists
	assignments *homeworkflow.Service
	deliveries  *senddelivery.Service
	rerunner    *homeworkflow.Rerunner
	materials   *homeworkflow.MaterialJobs // the summary and homework search of a library material (TASK-122)
	turnkey     *turnkey.Service           // triage_assignment and its Undo (TASK-129)
	answers     *answering.Service         // answer_question (TASK-118)
	prefs       *preferences.Service       // the free-work card setting (TASK-188)
}

// wireAgent builds the agent core (TASK-116, TASK-117, TASK-123, TASK-127, ADR-0021): the steps of a request on the
// Hub's AgentStep, the action journal with Undo and the Driver that runs the read tools, the reversible actions and
// the proposals (a confirm card only: the proposals service and the deletion preview, never their confirmation); its
// updates go to the agent panel of the UI (TASK-119). Today and the zone of the base come from the student's clock.
// It needs c.drafts and c.deletions.
func (c *core) wireAgent(e env, model agentturn.AgentModel, reads agentReads, log *slog.Logger) error {
	facts, err := agentloop.NewFacts(reads.knowledge)
	if err != nil {
		return fmt.Errorf("app: agent: %w", err)
	}
	facts = facts.WithMaterials(reads.materials).WithNotice(reads.prefs)
	subjects, err := agentloop.NewBaseSubjects(reads.library)
	if err != nil {
		return fmt.Errorf("app: agent: %w", err)
	}
	// The panel learns a request before its first Hub step returns, so «Стоп» works at once (ADR-0026); the progress
	// goes straight to the panel, not through the Driver, so the Driver and the step use case form no cycle.
	c.panel = wails.NewAgentPanel(log)
	progress, err := agentloop.NewProgress(panelSink{c.panel})
	if err != nil {
		return fmt.Errorf("app: agent: %w", err)
	}
	// The actions and their Undo run on the same use cases as the screens; checks of a target's version and the change
	// commit in one transaction of the local database. create_assignment and its Undo use the homework service of the
	// screens too (ADR-0029).
	actions, err := agentloop.NewActions(agentloop.ActionDeps{
		Library: reads.library, Lessons: reads.knowledge, Assignments: reads.assignments, NewAssignments: reads.assignments,
		Rerunner: reads.rerunner, Materials: reads.materials, Turnkey: reads.turnkey, Drafts: c.drafts, Jobs: reads.activity, Canceller: c.runner,
		Tx: c.db, Clock: e.local, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: agent actions: %w", err)
	}
	c.journal, err = agentjournal.New(agentjournal.Deps{
		Actions: sqlite.NewAgentActionRepository(c.db), Compensator: actions, Resolver: actions,
		Threads: sqlite.NewAgentThreadRepository(c.db), Processing: sqlite.NewJobRepository(c.db), Clock: e.clock,
		IDs: e.ids, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: agent journal: %w", err)
	}
	// «Повторить» of a finished request asks the journal whether the request left effects (ADR-0026); agentturn does
	// not import agentjournal (ARCH-20). Mentions are checked against the library, the schedule and the assignments.
	c.mentions = mentionObjects{
		subjects: reads.library, materials: reads.library, lessons: sqlite.NewLessonRepository(c.db),
		assignments: reads.assignments,
	}
	c.agent, err = agentturn.New(agentturn.Deps{
		Model: model, Threads: sqlite.NewAgentThreadRepository(c.db), History: sqlite.NewAgentHistory(c.db, e.clock),
		Subjects: subjects, Facts: facts, Tx: c.db, Clock: e.local, IDs: e.ids, Progress: progress, Effects: c.journal,
		Mentions: c.mentions, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: agent: %w", err)
	}
	// The chat list (ADR-0028) runs on the same rows; a rename or archive saved while an Ask loads the chat makes that
	// Ask go again on the stored chat (agentturn), so no lock is shared here.
	c.chats, err = agentchats.New(agentchats.Deps{
		Chats: sqlite.NewAgentThreadRepository(c.db), History: sqlite.NewAgentHistory(c.db, e.clock), Clock: e.clock,
		IDs: e.ids,
	})
	if err != nil {
		return fmt.Errorf("app: agent chats: %w", err)
	}
	c.panel.SetFills(c.chats) // the ring of a chat in the update that ends its request (ADR-0028)
	c.driver, err = agentloop.New(agentloop.Deps{
		Turns: c.agent, Sink: panelSink{c.panel}, Library: reads.library, Inspector: reads.library, Answerer: reads.answers,
		Knowledge: reads.knowledge, Activity: reads.activity, Schedule: reads.schedule,
		Assignments: reads.assignments, Deliveries: reads.deliveries, Journal: c.journal, Actions: actions,
		Proposals: reads.assignments, Deletions: c.deletions, Drafts: c.drafts, Clock: e.local, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: agent driver: %w", err)
	}
	// «Сделать» on the free-work card runs the topic triage through the same journaled action path (TASK-188); the
	// card's call is read from its own thread's history.
	c.freeWorks, err = agentloop.NewFreeWorks(agentloop.FreeWorkDeps{
		Threads: sqlite.NewAgentThreadRepository(c.db), History: sqlite.NewAgentHistory(c.db, e.clock), Journal: c.journal,
		Actions: actions, Settings: reads.prefs, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: agent free works: %w", err)
	}
	// A request left running stops as «нет связи»; a journal entry left PENDING is settled from its intent (DONE,
	// FAILED or UNKNOWN), one left UNDOING has its compensation retried.
	c.cleanups = append(c.cleanups,
		cleanup{name: "agent requests left running", run: c.agent.Recover},
		cleanup{name: "agent journal", run: func(ctx context.Context) (int, error) {
			rep, err := c.journal.Recover(ctx)
			return rep.Done + rep.Failed + rep.Unknown + rep.Undone + rep.Refused, err
		}})
	return nil
}

// panelSink hands the Driver's updates to the agent panel of the UI (TASK-119); mapping only, as input/wails does not
// import input/agent.
type panelSink struct {
	panel *wails.AgentPanel
}

// Publish forwards u.
func (s panelSink) Publish(u agentloop.Update) {
	s.panel.Publish(panelUpdate(u))
}

func panelUpdate(u agentloop.Update) wails.AgentUpdate {
	return wails.AgentUpdate{
		Thread: u.Thread, Turn: u.Turn, Scope: u.Scope, Status: u.Status, StopReason: u.StopReason, Cards: u.Cards,
		Problem: string(u.Problem), Progress: u.Progress,
	}
}

// driverCurrent reads the current request of a chat through the Driver for the panel (TASK-272); mapping only, as
// panelSink.
type driverCurrent struct {
	driver *agentloop.Driver
}

// Current maps the Driver's Current.
func (d driverCurrent) Current(ctx context.Context, id shared.ThreadID) (wails.AgentUpdate, bool, error) {
	u, ok, err := d.driver.Current(ctx, id)
	return panelUpdate(u), ok, err
}

// Draft forwards d.
func (s panelSink) Draft(d agentloop.Draft) {
	s.panel.Draft(wails.AgentDraft{
		Thread: d.Thread, Turn: d.Turn, CallID: d.CallID, Texts: d.Texts, End: wails.AgentDraftEnd(d.End),
	})
}
