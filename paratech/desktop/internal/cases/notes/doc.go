// Package notes is the use case of the student's own notes of a lecture («Мой конспект», ADR-0027, TASK-151): attach
// files or pasted text to a recorded lecture, a schedule lesson, a date or «без даты», replace a part, reorder, move
// to another lecture, rename, remove and restore, and read the notes of a lecture or a subject.
//
// Modelling choices:
//   - The files are materials of the library (ADR-0024), stored and filed through the Materials port that
//     library.Service implements (no use case imports another, ARCH-20): the same bytes are one material, and a part
//     that arrives in «Входящие» is filed under «Лекции», «Семинары» or «Лабораторные» by the lesson kind. Every part
//     must be a material of the note's subject: a material filed in another subject, or a material ID from outside, is
//     checked and refused (ErrOtherSubject).
//   - Add and Paste without a note add to the first active note of the same subject at that lecture, so a second drop
//     on a lecture extends its notes; a lecture «без даты» always gets a new note. A file that is a part already is
//     «уже добавлено» and gives no second part.
//   - An anchor is checked when it is set: a recording must be COMPLETED and not marked for deletion, a lesson must be
//     in the schedule. At read time a recording or lesson that is gone reads as the day of its snapshot; nothing is
//     written back, and deleting a recording never deletes a note (ADR-0018 is unchanged).
//   - The lists of the repository hold removed notes too: ForLesson and ForSubject return the active ones only.
//   - Pasted text becomes a TXT material of at most MaxPastedBytes.
//
// File names are display text only; paths never reach this package. Rules that apply: ARCH-20..27.
package notes
