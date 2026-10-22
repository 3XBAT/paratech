# TASK-121 — desktop library (subject, material, cases/library, migration 0021)

Done: domain/library (subject with system/own folders, material with content-based type, values, subjectmatch), cases/library
(import with sha256 dedup into the inbox, place, link, soft delete, suggestions), sqlite repos + migration 0021,
input/wails LibraryService (native file dialog only), knowledge base groups by subject id via a resolver port. Tests at every layer.

Decisions
- Free subject strings (ADR-0024 open item): no backfill, no rewrite. A string resolves to a subject id at read time via subjectmatch
  (name or alias); unresolved strings keep legacy grouping. SuggestSubjects lists unmatched strings from the schedule and recordings, never creates.
- Inbox is a library-wide inbox, not a subject folder; one material row per sha256 (re-upload returns it, a removed one returns to the inbox).
- "Empty" for folder/subject removal counts removed files too.
- Out-of-scope but needed: shared ids, artifact kind materials, random ids, app wiring, domain knowledgebase LibrarySubject grouping.
- Existing tests changed (additive only): migrate_test, shared_test, random_test, shell_test, settings_test, tests/schema_test.

Follow-ups (reviewer mediums, deferred)
- Lesson purge cascades material_lessons without bumping materials.version: fix before TASK-123 Undo (bump version in purge or document).
- Name vs other subject's alias uniqueness only in the use case; a concurrent create can make a string ambiguous (safe: matches nothing). Consider a subject_keys table.
- library.Service used as port implementation of knowledgebase (app wiring): one sentence in ADR/architecture; ADR-0024 open item still to mark as settled.
- ensureBlob trusts size only: re-hash when TASK-122 adds text extraction. Two key normalizers (kb.SubjectKey vs values.Key): drift risk.
- Not done: frontend, Inspect/text extraction, open in Word, rename/remove alias, blob purge, drag-and-drop upload.
Security: no findings (two lows above noted).
