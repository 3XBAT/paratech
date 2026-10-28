# TASK-311 — binding for subjects from the schedule

## Done
- `input/wails/library_schedule.go`: `LibraryService.ScheduleSubjects()`, `CreateFromSchedule(token, choices)`, `UndoFromSchedule(token)`; optional port `LibraryScheduleUseCase` (satisfied by `library.Service`, attached by a type assertion in `NewLibraryService`; nothing changed in `app` except a wiring test).
- `frontend/src/bindings/library.ts`: types, strict parsers (unknown problem/action/kind/link/reason, wrong type, missing field or null list = parse error with a fixed text, no reply value in it), `scheduleKindWords`, code lists for TASK-312.
- Replies carry a `problem` code instead of an error; no error text and no schedule string reaches the page except as labels (names, spellings).

## Contract for TASK-312 / TASK-323
- `ScheduleSubjects` -> `{token, groups, omitted, problem: '' | unavailable}`; group `{key, name, aliases, kinds: [{kind: lecture|seminar|lab|unknown, lessons}], lessons, recordings, doubtful, members: [{key, name, spellings, kinds, lessons, recordings, doubtful, link: same|abbreviation|typo}], action: create|alias_of, subjectId, subjectName}`. `omitted` = rows cut beyond 200: show a short note.
- `CreateFromSchedule` -> `{created:[{id,key,name}], aliased:[{subjectId,aliases}], skipped:[{key,subjectId,reason: name_taken|too_many_aliases|subject_gone}], problem: '' | stale | invalid | name_taken | busy | unavailable}`. The done part comes with a problem too (failure midway). `name_taken` as problem = a rename collided: show it next to the name field.
- `UndoFromSchedule(token)` -> `{removed, kept:[{id,name,reason: has_files|has_notes}], unaliased, keptAliases:[{subjectId,aliases,reason: stale}], problem: '' | invalid | busy | unavailable}`.
- UI notes: offer «Отменить всё» only when `created + aliased` is not empty; keep the token and still offer it after `unavailable`/an unreadable reply (the journal has the done part); `invalid` on undo also means «token unknown» (app restarted or 8 newer packages): say «Отменить уже нельзя, предметы можно удалить вручную» with the way to the page of subjects, no dead end; `busy` = «ещё идёт, повторите через секунду»; `stale` = call `ScheduleSubjects` again.

## Decisions to confirm
- **Deviation from tasks.json (supervisor decision):** `UndoFromSchedule(token)` takes no result. The Go side keeps an in-memory journal token -> result (last 8 tokens, mutex) written from every `CreateFromSchedule` outcome that created or aliased something (also the done part of a failed call); a retry MERGES into the entry (union by ID / per subject spellings; reviewer medium, fixed). The use case still validates every ID and spelling against the token. Lost on restart: undo then answers `invalid`.
- `busy` added to the problem codes: one create/undo at a time (atomic guard, no shared lock; `ScheduleSubjects` and other library calls stay callable); a second call answers at once. 30 s ctx timeout per call.
- Extra problem `name_taken`; extra fields: member `spellings`, `recordings`, `doubtful`, `link`, member choice `name` (rename for `separate`).
- Choices capped at 256 (and 256 members per group). Reasons go through Go-side whitelists with fallbacks (skipped -> name_taken, kept -> has_files, kept spellings -> stale); the count of fallbacks is logged.
- The `unavailable` cause is not logged at all (a use case error could carry a schedule string): less to diagnose.

## Follow-ups
- Low (reviewer): spellings merged by exact text, not by key; a union of retries with different choices could pass the use case's 200-entry caps and make the undo `invalid` (very unlikely). The page sees only the latest call's result; only the journal holds the merged one.
- The journal is not persisted; if undo must survive a restart, store it locally (a later task).

## Changed existing tests
`frontend/test/library.test.mjs` ("bound method names and error texts match the Go LibraryService"): `library_schedule.go` added to the list of Go files the test reads, since the new methods live there.

## Security
Auditor: no High/Medium; notes on retry replace (fixed by the merge) and on undo reading the journal before the guard (fixed). Reviewer: one medium (journal replace, fixed) and three lows (fixed); re-review of the fix commit: approve.
