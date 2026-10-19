// Package recordingdelete is the use case of deleting recordings (TASK-095, ADR-0018): one or several at once, with
// the sound, the transcript and its live parts, the summary, the lecture text, the jobs of the recording and the found
// homework the user chose.
//
// A deletion runs in this order, each step repeatable, so a crash anywhere is finished by the next start (Resume):
//  1. Mark: one transaction marks the recording (recording.MarkDeleting). From then on it is hidden, never
//     transcribed or assembled, and its choice of assignments is stored.
//  2. Jobs: the unfinished jobs of the recording (and the draft jobs of the chosen assignments) are cancelled through
//     the job runner, and the use case waits until none runs. A job claimed in the meantime is found by the next look.
//  3. Files: the chunks, the track and the transcript documents go through the Storage Manager.
//  4. Purge: one transaction checks again that no job is unfinished and removes the rows (ADR-0018 extends the
//     exception of ADR-0004(a)). Each deleted assignment leaves a deadline event removal that the calendar sync
//     carries out; no Google call runs here.
//
// Rules that apply: ARCH-20..27.
package recordingdelete
