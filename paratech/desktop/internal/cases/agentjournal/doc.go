// Package agentjournal is the use case of the agent action journal and Undo (ADR-0021): record a call before it
// runs, record its outcome, undo with a typed compensation, recover entries left by a crash.
//
// Only the ports exist yet (TASK-115); the scenarios come with TASK-116.
//
// Rules that apply: ARCH-20..27. Two short transactions per action; the status machine lives in domain/agent/action.
package agentjournal
