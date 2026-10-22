// Package action is the aggregate of the agent action journal with Undo (ADR-0021, design §2.4).
//
// A U call is recorded PENDING before its use case runs and gets its outcome after (two short transactions, the use
// case between them idempotent by Key, namespaced by thread and action). A C call is recorded PROPOSED with its
// proposal or preview; its status is read from the proposal.
//
// Status machine (transitions.go):
//
//	PENDING -> DONE | FAILED | UNKNOWN (startup recovery: no Undo)
//	DONE    -> UNDOING
//	UNDOING -> UNDONE | UNDO_REFUSED (the target changed since the action)
//
// PROPOSED, FAILED, UNKNOWN, UNDONE and UNDO_REFUSED are terminal; UNDOING left by a crash is retried. A DONE action
// carries a typed Compensation that matches its tool and the target version Undo compares with.
package action
