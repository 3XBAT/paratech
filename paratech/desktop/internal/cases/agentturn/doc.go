// Package agentturn is the use case of one agent step (ADR-0021): load the chat (ADR-0028), open a step, call
// the planner through AgentModel, parse the calls in the domain, apply the policy and the budget, save the thread.
//
// The case runs no tool: it returns the admitted calls (Step.Calls) and the Driver of input/agent runs them and
// reports the results with Submit.
//
// Keys of a step (TASK-116 decision d, ADR-0026 addendum 2026-11-08): ag:<thread>:<turn>:<step>. A transient failure
// is retried by itself under the SAME key (backoff.Interactive). «Продолжить» (Continue) repeats the same key only
// after a stop by the student or the quota, so an answer the Hub already produced is replayed for free. A step stopped
// offline or failed after the automatic retries needs «Повторить» (Retry) under ag:<thread>:<turn>:<step>:r<N>; an
// offline stop with no open step (calls waited for the Driver) continues. When the Hub no longer keeps the model
// turn (values.ErrTurnGone) the request starts again from the student's text: by itself while nothing was done yet,
// else by Restart. «Повторить» of a finished or stopped request (Rerun, ADR-0026) starts a new request with the same
// text and scope under ag:<thread>:<turn+1>:<step>, refused while the request has live effects (port Effects).
//
// Rules that apply: ARCH-20..27. Decisions (class, plan, provenance, budget) are made by the agent domain, not here.
package agentturn
