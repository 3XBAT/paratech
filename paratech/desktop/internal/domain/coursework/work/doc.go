// Package work is the aggregate of a turnkey work («ДЗ под ключ», TASK-129, ADR-0025): one assignment (or an uploaded
// file with it, or a topic without an assignment, TASK-187) from the triage by the quarantined reader
// TRIAGE_ASSIGNMENT to the confirmed requirements, then the writing (TASK-105) or the helper mode when the app cannot
// do the work whole. Only a work of an assignment may be sent to a teacher (Sendable).
//
// The status machine is a table (transitions.go): triage → questions (rounds 1 and 2 only) → triage of the next round
// → review («Как я понял задание», the student edits and confirms) → confirmed → writing → done; an impossible work
// goes to helper, a reader that keeps refusing to failed. The third reader call is final: no status allows a third
// question round.
//
// Everything the reader wrote (requirements, questions, assumptions, the helper plan) is untrusted data: it is
// checked strictly against the reader schema (triage.go), shown in cards and never acts (hard rule 3). The student's
// answers and edits are local data.
package work
