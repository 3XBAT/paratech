// Package thread is the aggregate of an agent thread (ADR-0021, design §2.2, §2.4): the thread of one subject or of
// no subject (key NONE), its current request (turn) with the step counter, budget, question rounds and plan, and the
// IDs seen in tool results of this thread (provenance).
//
// Turn status machine (transitions.go):
//
//	PLANNING         -> AWAITING_PLAN | AWAITING_ANSWERS | EXECUTING | DONE | STOPPED | FAILED | CANCELLED
//	AWAITING_PLAN    -> EXECUTING (approved) | PLANNING («Изменить») | CANCELLED
//	AWAITING_ANSWERS -> PLANNING (answered) | CANCELLED
//	EXECUTING        -> AWAITING_PLAN (new plan) | DONE | STOPPED | FAILED | CANCELLED
//	STOPPED          -> PLANNING | EXECUTING («Продолжить», back where it stopped) | CANCELLED
//
// DONE, FAILED and CANCELLED are terminal. A request over budget stops itself (STOPPED, reason budget) and cannot be
// continued. A step is opened before the Hub call and closed with its usage; a stopped request keeps its open step,
// so «Продолжить» repeats the same key ag:<thread>:<turn>:<step> (free replay). A step that failed for good (STOPPED,
// reason failed) is sent again only by «Повторить» (Retry) under ag:<thread>:<turn>:<step>:r<N>; a request whose model
// turn expired on the Hub starts again from its text (Restart, HistoryFrom).
//
// Every call of a step is answered before the next step (Expect, Answer, TakeResults), because the Hub accepts an
// echo of calls only followed by the results of exactly those calls. IDs created by approved create steps are kept
// per request, so a deferred plan argument binds only to one of them from an earlier step.
//
// Older turns are not part of the aggregate: the history sent to the planner is chosen by WindowStart.
package thread
