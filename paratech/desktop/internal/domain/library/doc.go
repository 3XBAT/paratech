// Package library is the bounded context of the student's subjects and the files filed under them (ADR-0024).
// Aggregates: domain/library/subject (name, aliases, semester and folders), domain/library/material (one file of
// the library by its sha256, and its extracted text) and domain/library/inspection (the reading of a material by the
// quarantined reader, ADR-0022: suggestions only). Value objects shared by them live in domain/library/values (the
// semester, the matching key of a name). Domain service: domain/library/services/subjectmatch (a free subject string → subject id, and the
// subjects to suggest from the schedule); domain/library/services/overview (the rules of the page of a subject).
//
// The library is virtual: folders live in SQLite, the bytes of a file sit once in the private artifact store under
// their sha256. Nothing of it is a visible folder on disk.
//
// Layout of a context (docs/architecture.md section 3.1):
//
//	library/doc.go                  this file: purpose and glossary
//	library/<aggregate>/            one package per aggregate
//	library/values/                 value objects shared by several aggregates of the context
//	library/services/<service>/     domain services and policies
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - Subject: a course the student takes, created by the student (never automatically). Its name and aliases are
//     unique across all subjects by their key (case-insensitive, «ё» = «е», spaces collapsed).
//   - Alias: another spelling of a subject, for example the subject string of the schedule.
//   - Semester: autumn (September to January) or spring (February to August) of a year, derived from a date.
//   - System folder: one of Лекции, Семинары, Лабораторные, Материалы, ДЗ, Билеты; every subject has all of them and
//     none can be removed. Own folder: a folder the student added; removed only while it holds no file.
//   - Material: one file of the library: its sha256, size, detected type (docx, pptx, txt, pdf) and the original name,
//     shown only (never sent to a model). A material is active or removed (soft delete, restorable).
//   - «Входящие» (inbox): where a new upload lands. It is not a folder of any subject: a material without a subject
//     and folder is in the inbox. Placing files it under a subject; undoing a placement returns it to the inbox.
//   - Deduplication: one blob and one material per sha256. Uploading the same bytes again returns the material that
//     exists (a removed one comes back into the inbox).
//   - Free subject string: the subject a schedule entry or a recording names as text (ADR-0019). It is resolved to a
//     subject id when read (subjectmatch); stored rows are never rewritten.
package library
