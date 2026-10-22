// Package agentturn is the use case of one agent step (ADR-0021): load the thread of the scope, open a step, call
// the planner through AgentModel, parse the calls in the domain, apply the policy and the budget, save the thread.
//
// Only the ports exist yet (TASK-115); the scenarios come with TASK-116.
//
// Rules that apply: ARCH-20..27. Decisions (class, plan, provenance, budget) are made by the agent domain, not here.
package agentturn
