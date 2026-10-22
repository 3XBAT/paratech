// Package agentturn is the use case of one agent step (ADR-0021): load the thread of the scope, open a step, call
// the planner through AgentModel, parse the calls in the domain, apply the policy and the budget, save the thread.
//
// The case runs no tool: it returns the admitted calls (Step.Calls) and the Driver of input/agent runs them and
// reports the results with Submit.
//
// Rules that apply: ARCH-20..27. Decisions (class, plan, provenance, budget) are made by the agent domain, not here.
package agentturn
