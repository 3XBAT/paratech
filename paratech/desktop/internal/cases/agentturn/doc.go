// Package agentturn is the use case of one agent step (ADR-0021): load the thread of the scope, open a step, call
// the planner through AgentModel, parse the calls in the domain, apply the policy and the budget, save the thread.
//
// The case runs no tool: it returns the admitted calls (Step.Calls) and the Driver of input/agent runs them and
// reports the results with Submit.
//
// Keys of a step (TASK-116 decision d): ag:<thread>:<turn>:<step>. «Продолжить» (Continue) after a stop by the
// student, a lost connection, a timeout, a busy Hub or the quota repeats the SAME key, so an answer the Hub already
// produced is replayed for free. «Повторить» (Retry) after a failure the Hub keeps for its key (a refusal, a provider
// error without retry, an unusable reply) sends ag:<thread>:<turn>:<step>:r<N>. When the Hub no longer keeps the model
// turn (values.ErrTurnGone) the request starts again from the student's text: by itself while nothing was done yet,
// else by Restart.
//
// Rules that apply: ARCH-20..27. Decisions (class, plan, provenance, budget) are made by the agent domain, not here.
package agentturn
