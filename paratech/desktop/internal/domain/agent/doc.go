// Package agent is the bounded context of the agent panel (ADR-0021, ADR-0022): threads per subject, requests and
// their steps, the tool catalog, the action policy and the action journal with Undo.
// Aggregates: domain/agent/thread, domain/agent/action, domain/agent/post (the post after a lesson, TASK-125). Values:
// domain/agent/values. Services: domain/agent/services/actionpolicy, domain/agent/services/today (rules of «Сегодня»).
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared; views of other contexts
// reach it as agent-owned values (Ref, Scope, Base) filled by use cases.
//
// # Glossary
//
//   - Thread: the conversation of one subject, or of no subject (key NONE). Only the current subject's thread is sent
//     to the planner.
//   - Turn (request): one request of the student and the steps it takes until DONE, STOPPED, FAILED or CANCELLED.
//   - Step: one planner call on the Hub with the key ag:<thread>:<turn>:<step>; it returns 1 to 4 tool calls.
//   - Tool: a closed catalog entry (golden list shared/testdata/tool_catalog_v4.json) with strictly typed arguments.
//   - Class: R read-only, U reversible local (done at once, with Undo), C irreversible (proposal card, the student
//     clicks), UI card only.
//   - Plan: approved list of steps; later writes of the request must match an unused approved step.
//   - Provenance: an ID in arguments must have appeared in a field of the same ID kind in the base, the scope or the
//     tool results of this thread; the assignment IDs a reply lists pass the same check.
//   - Budget: per request limits of steps, calls per step, writes, paid jobs without a plan, input tokens and
//     question rounds.
//   - Action: a journal entry of a U call (PENDING, DONE, FAILED, UNKNOWN, UNDOING, UNDONE, UNDO_REFUSED) or of a C
//     call (PROPOSED), with a typed compensation used by Undo.
package agent
