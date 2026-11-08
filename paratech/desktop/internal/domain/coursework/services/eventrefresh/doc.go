// Package eventrefresh decides what a deadline change does to the Google deadline event of an assignment (TASK-276,
// ADR-0029 addendum 2 «Deadline change and the Google event», З3). It is pure: no I/O, no clock.
//
// The policy is this context's own enum (ARCH-11): the use case maps the student's setting, or the fixed Update of
// the agent's edit_due (addendum 3), to it. The result is acted on by the use case without a condition of its own
// (ARCH-22): Ask returns the question, QueueUpdate and QueueRemoval write a row of the calendar sync queues.
//
// Rules that apply: ARCH-10, ARCH-15, ARCH-17.
package eventrefresh
