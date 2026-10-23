// Package turnkey is the use case of a turnkey work («ДЗ под ключ», TASK-129, ADR-0025): the triage of an assignment
// by the quarantined reader TRIAGE_ASSIGNMENT, the student's answers to its questions (at most two rounds, then a final
// call with stated assumptions), the edits and the confirmation of «Как я понял задание», and the helper mode with
// «Спросить по шагу». The rules live in the aggregate coursework/work; this package loads, calls the reader, saves.
//
// The reader's answer is untrusted data (hard rule 3): it is parsed strictly, stored and shown, and never acts. The
// agent reaches Triage through input/agent (the tool triage_assignment); the student reaches the rest through
// input/wails.
package turnkey
