// Package knowledgebase holds the rules of the knowledge base (TASK-091): which subject and kind a lesson shows,
// how lessons are grouped by subject, and the search over titles, summaries, lecture texts and transcripts. It is pure:
// no I/O, no clock; the use case passes plain values in, so the package needs no other context (ARCH-11).
//
// There is no index: a search scans what is stored, so a deleted recording leaves the base with its rows (ADR-0019).
// Every text is untrusted (imported schedule text, model output, recognised speech): it is matched and cut as plain
// text, never interpreted.
//
// Rules that apply: ARCH-10, ARCH-15, ARCH-17.
package knowledgebase
