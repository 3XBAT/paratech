// Package library is the use case of the subjects and their files (TASK-121, ADR-0024): list, create and remove
// subjects and own folders, upload a file, file it under a subject, link it to lessons, remove and restore it, and
// suggest subjects from the free subject strings of the schedule and the recordings.
//
// Modelling choices:
//   - «Входящие» is the inbox of the whole library, not a folder of a subject: a material without a subject and folder
//     is in it. Every upload lands there; MoveToInbox (the undo of Place) returns a file there. It cannot be removed.
//   - Deduplication is by sha256: one blob (materials/<sha256> in the artifact store) and one material per sha256.
//     Uploading the same bytes again returns the material that exists; a removed one comes back into the inbox.
//   - Free subject strings are never rewritten and subjects are never created from them: ResolveSubjects maps a string
//     to a subject id when it is read (subjectmatch: name or alias, an ambiguous one matches none); SuggestSubjects
//     lists the strings that match no subject, and the student creates the subject (optionally with the string as an
//     alias).
//   - A subject or folder is removed only when empty: the use case counts the files (active and removed; a removed
//     file needs its place to be restored) and the aggregate refuses a non-empty one; the database RESTRICT closes the
//     race between the count and the save.
//
// The text of a material (TASK-122): MaterialText reads the stored bytes again, checks them against the sha256 and
// extracts the text; Inspect sends the beginning of it with the subjects as candidates to the quarantined reader on
// the Hub (ADR-0022) and keeps the strictly checked answer per material and text. That answer is a suggestion only:
// nothing is placed, linked or sent because of it. Inspect is the only call that talks to the network. The original
// file name is shown only, never logged or sent.
//
// Rules that apply: ARCH-20..27.
package library
