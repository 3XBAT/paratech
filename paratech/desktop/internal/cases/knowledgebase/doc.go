// Package knowledgebase is the use case of the knowledge base (TASK-091): the subjects of the recorded lessons with
// how many lessons each has, the lessons of a subject with what is ready (summary, lecture text, transcript), the
// search over titles, summaries, lecture texts and transcripts, the export of a whole subject to one Markdown file the
// user chose, and the user's correction of the subject and kind of a recording. Opening a lesson is the summary and
// transcript viewers' job (summaryview, transcriptview), by recording id.
//
// Everything is local and read at query time, without an index (ADR-0019): a recording marked for deletion is left
// out, a purged one is gone. Summaries, lecture texts and transcripts are untrusted text (hard rule 3): they are
// returned and written as data, never logged, and nothing here has a side effect but the file the user chose.
//
// Rules that apply: ARCH-20..27.
package knowledgebase
