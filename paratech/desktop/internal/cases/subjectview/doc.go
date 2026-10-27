// Package subjectview is the use case of the page of a subject (TASK-225, docs/design/concept-agent.html
// tpl-subjects): Overview reads «Ближайшее» (the nearest deadlines of the subject), the schedule line, the latest
// recorded lessons with their status and the «новое» mark of the files and summaries that appeared since the page was
// last opened. MarkOpened records an opening; Overview itself changes nothing.
//
// Lessons, schedule entries and assignments name their subject as a free string; it is resolved to the library
// subject when read (ADR-0024), never rewritten. Everything is local: it works offline.
//
// Rules that apply: ARCH-20..27. The rules of the page are domain/library/services/overview.
package subjectview
