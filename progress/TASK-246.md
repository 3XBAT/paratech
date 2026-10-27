# TASK-246 — search of objects for an @-mention

## Done
- `AgentService.MentionCandidates(query)` in `input/wails/agentmentions.go`: at most 20 `{kind, id, label, hint}` over library subjects (name or alias), lessons (knowledge-base lectures tied to a schedule lesson, checked with the existing `mentionObjects.Exists`), active materials and non-deleted assignments. Empty query = newest activity first. Reply `{problem: ""|"unavailable"|"failed", candidates}`.
- Narrow interfaces declared in input/wails; wiring in `app/agent.go`, `app/core.go` (the existing `mentionObjects` is reused, nothing duplicated).
- Labels and hints: control/bidi/zero-width stripped, cut to 120 runes; material names keep only the last path segment; query cut to 100 runes; caps: 500 objects per source, 100 subject groups, 40 lesson checks.
- `frontend/src/bindings/agent.ts`: `mentionCandidates` + `parseMentions` (drops bad items, cap 20).

## Decisions to confirm
- Matching/ordering lives in input/wails as the task states (ARCH-41 judged acceptable by the reviewer); move to a `cases/mentionsearch` use case if the search grows.
- ADR-0028 Open 2 untouched: only existing objects are searched.
- Lectures without a schedule lesson are not offered (a `lesson` mention needs a LessonID).
- Only the newest `MaxListedAssignments` assignments are searchable.

## Follow-ups
- Medium: each search calls `knowledgebase.Lectures` per subject group (up to 101 full rebuilds). Add a single-pass `AllLectures` in cases/knowledgebase; TASK-247 must debounce the «@» input.
- Low: a lecture known only from notes gets a hint without date (`mentionHint` should use the note date `at`); no Go test for notes-only lectures.

## Changed existing tests
- `frontend/test/agent.test.mjs`: the Go-method-name check also reads `agentmentions.go`.

## Security
Auditor: no findings. No paths or contents in the DTO, labels sanitized, query only matched in memory, all reads capped.
