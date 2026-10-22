// Package agent is the loop of the agent panel (ADR-0021): Driver runs a request step by step on cases/agentturn,
// runs the calls a step admits through its own narrow interfaces on the use cases (ARCH-42) and shows the cards
// through a Sink. It makes no decisions: class, plan, provenance and budget come from agentturn and the agent domain
// (ARCH-41). Results for the planner are built by the mappers of domain/agent/values from IDs, dates, enums and
// counters only (ADR-0022).
//
// input/wails does not import this package: it declares its own interface and app plugs the Driver in. The Driver
// has no method that confirms, executes, deletes or sends anything: irreversible actions are out of the agent's
// reach (ADR-0021, ADR-0022).
package agent
