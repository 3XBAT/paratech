// Package subjectpurge is the use case that erases a removed subject once its restore window has passed (TASK-308,
// ADR-0027 «Purge after 3 days»): its notes, its ticket sets, the files filed under it with their stored bytes, then
// the subject row with its aliases, folders and teachers. It runs at the start and once a day, and the library runs it
// when an expired subject still holds a name the student wants.
//
// Every step is its own transaction and repeatable, and every delete re-checks in the store that the subject is still
// removed and past its window, so a crash anywhere is finished by the next run and a restore that won the race keeps
// everything. Assignments, works (their source files move to «Входящие»), recordings, schedule lessons, answers and
// the agent journal are other contexts and stay.
//
// Rules that apply: ARCH-20..27.
package subjectpurge
