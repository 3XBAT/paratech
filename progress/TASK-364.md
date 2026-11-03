# TASK-364 — ссылка на онлайн-пару у предмета: «Подключиться» на «Сегодня»

## Done
- Domain `library/subject`: value `CallLink` with its own copy of the ADR-0032 rule (ARCH-11), `SetCallLink`/`ClearCallLink` on the aggregate; a stored value that no longer parses is read as no link; the link is never printed (String/LogValue/Format). Cross-check test `desktop/tests/calllink_crosscheck_test.go` keeps it equal to `schedule/values.ParseCallLink`.
- SQLite: migration `0050_subject_call_link.sql` (`subjects.call_link`, CHECK <= 2048), sqlc v1.31.1, `docs/contracts/schema.sql`.
- `cases/library`: set / clear / `CallLinks`, `OpenCallLink(id)` (re-read at the click, port `CallOpener` implemented by `adapters/weblink` `OpenSubjectCallLink`, which re-parses with the schedule rule and needs the canonical form), `CreateSubjectWithCallLink` (link checked first, one save).
- `cases/todayfeed`: lesson rows carry `subjectId`, `subjectState` (none/unknown/ambiguous/known/unavailable), `hasCallLink`, `callLinkHost`; a failed library read keeps the day (best effort, one subject lookup).
- `input/wails`: set/clear/open/create-with-link bindings (page sends only the subject ID to open); Today DTO has the host only.
- Agent: `list_subjects` rows carry `call_link` (only when set, light shape check, dropped before any subject is cut); no new tool, catalog stays 9. Hub panel v9 prompt edited in place (small: Trust exception for the `call_link` of a list_subjects row, link-request rule, general "never send to a screen that lacks it / never promise what the app cannot do" rule, screens lines; «Расписание» holds no links).
- UI: «Подключиться» / «Добавить ссылку» on the Today pair card (all pairs of the day, quiet), notes instead of buttons for none/ambiguous/unavailable, create-subject flow for an unknown subject string, block «Ссылка на пару» on the subject page (show, change, delete + «Вернуть»). QA: `docs/qa/tasks/TASK-364.md`.
- ADR-0032 addendum: link per subject only for now (owner 2026-11-03); lesson link and calendar link postponed; Google Calendar not read.

## Decisions to confirm
- A Zoom link often carries `?pwd=`: via `list_subjects` it reaches the Hub and the LLM provider and can stay in the local agent history (owner О14 = а; ADR addendum). Reply text may repeat the `call_link` (narrow prompt exception; the Core has no URL filter on reply text, replies are plain text, never linkified).
- Saving a link has no version check (last save wins), because the Today card does not know the subject version.
- Creating a subject from the Today card (unknown subject string) is beyond the literal AC; added to avoid a dead end. Name taken / removed name handled with «Вернуть предмет и сохранить».
- «Подключиться» / «Добавить ссылку» shown for every pair of the day, not only in the «Записать» window ("always at hand"). `Visible` window of ADR-0032 not used.
- When the library cannot be read, today's posts show under their lesson string with an empty SubjectID until the next read.
- No auto-adding `https://` to a typed `example.com` (refused with a plain text).
- The Hub with the updated v9 prompt must be redeployed before the agent QA step works.

## Follow-ups
- Saving a link from chat needs a new catalog tool (`set_call_link`, catalog 11 in `docs/agent-parity.md`); not added (catalog 10 is TASK-326). `docs/agent-parity.md` line 36 (link at the subject) is out of date.
- `list_subjects` JSON writes `&` as the escape `&` (Go default; shared encoder left unchanged).
- Per-lesson student link, calendar link, `joinlink`/`Visible` window of ADR-0032 remain open.
- Suggestions not applied: copy button on the subject page, copy fallback when opening fails, busy-label debounce, rate-limited Warn when the library is unreadable.
- TASK-366 edits the same prompt: this task's lines are self-contained (Trust exception, two lines after "Name only screens", appended sentences in «Сегодня» / «Предметы» / «Расписание», one "Where things are" item).

## Changed existing tests
`migrate_test.go` (schema version 50); `today_sqlite_test.go` and todayfeed fakes (resolver `LookupSubjects`, new link port); `TestTodayCallLinkErrors` replaced by the degraded-path tests (a failed read no longer fails Today); wails `today_test.go` (subjectState); `frontend/test/library.test.mjs` (Go method scan reads `library_calllink.go`); texts-only changes in `callLink364ui.test.mjs` after the UX rounds.

## Review
Reviewer: changes requested (ARCH-41/42 opening in input, create-before-validate, prompt promises, missing progress) -> fixed; re-review: approve. Security-auditor: no findings twice (first diff and fix commits). ux-expert: «доработать» twice; all must-fix items fixed (second round A-D fixed after the last review, small and tested, not re-reviewed).
