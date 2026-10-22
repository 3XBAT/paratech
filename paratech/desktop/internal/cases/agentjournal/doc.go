// Package agentjournal is the use case of the agent action journal and Undo (ADR-0021): record a call before it
// runs, record its outcome, undo with a typed compensation, recover entries left by a crash.
//
// The Driver of input/agent calls Begin before a U call runs and Finish after it, Propose for a C call, and Undo
// for «Отменить»; app runs Recover at start.
//
// Rules that apply: ARCH-20..27. Two short transactions per action; the status machine lives in domain/agent/action.
package agentjournal
