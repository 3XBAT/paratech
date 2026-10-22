# ADR-0019: Knowledge base without an index

- **Status:** Accepted
- **Date:** 2026-10-20
- **Source:** TASK-091; PRD §2.2, §18 (no vector DB); ADR-0018.

## Context
«База знаний» lists lessons by subject and searches titles, summaries, lecture texts and transcripts. A student has
tens to a few hundred lessons; transcripts are JSON files in the artifact store.

## Decision
- **No index, no new dependency.** A search scans at query time: rows from SQLite, transcripts through the Storage
  Manager, matched in the pure service `domain/coursework/services/knowledgebase`: case-insensitive, «ё» = «е»,
  spaces collapsed, 2–100 characters, at most 3 hits per section of a lesson and 50 in all, snippet of 120 characters
  as one plain line. A broken transcript file, summary or lecture text row is skipped and logged. FTS5 was not needed.
- **Deletion needs nothing.** A recording marked for deletion is left out; the purge of ADR-0018 removes the rest.
- **Lessons** are completed, not silent recordings (failed and silent ones hold no text). Subject and kind: the user's
  correction per field, else the snapshot as a whole, else the live lesson (the rule of assignments,
  `values.ResolveScheduledLesson`). Subjects are grouped case-insensitively; empty is «Без предмета».
  *Amended by ADR-0024*: lessons are grouped by subject ID of the `library` context, not by the free string.
- **Correction** is stored on the recording (`user_subject`, `user_kind`, migration 0019) through
  `Recording.CorrectLesson`; the snapshot stays write-once. Empty clears it.
- **Export** of a subject: one Markdown file, lessons oldest first, each with its summary and lecture text. Transcripts
  are left out: the lecture text is the full text of the lesson.

## Consequences
- A search reads and parses every transcript of the base: measured about 3 s for 200 three-hour lessons, under a
  second for a semester. The page searches on demand, not per keystroke. If it gets slow, cache parsed transcripts or
  add FTS5 in a new ADR.
