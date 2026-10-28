# TASK-296 — sources of an answer on the agent answer card

## Done
- `bindings/answers.ts`: `answer(id)` parses the whole `AnswerView` strictly (lists must be arrays, cite index inside sources, section one of summary / lectureText / transcript / ownNote, own-note ids and locator consistent; a removed source never fails the card). Errors map to the two fixed texts of `answers.go`.
- `agent/AnswerSources.tsx` (new) under the `answer` card: loads once per `answerId`, lists sources in the core's order; label via `citationPlace` as a plain text node, lesson day and title as a muted second line. An openable row is a `<button>` with `aria-label` «Открыть: …»; a removed row is plain muted text «источник удалён». Loading «Загрузка источников…»; any failure «Источники не загрузились.»; no `answerId`, no API or no sources: the card is as before. Claims and quotes are parsed, not rendered (not in this task).
- Click: `openSource` goes `AgentPanel` -> `App` (route «База знаний», pending jump, `jump` counter remounts) -> `KnowledgeBasePage` (`jump` prop, taken once). Recording source: `lesson(recordingId)` and the focus of `citationOpening`. Own note: `findNoteLecture` scans `subjects()` then `lectures(key)` (first 101 subjects) for the lecture that holds `noteId`, opens «Мой конспект» with the file (`materialId`) marked. Not found: «Конспект не найден: обновите список.». While resolving: «Открывается…»; any other navigation of the student cancels the jump (`jumpTicket`/`runJump`, pure, tested).
- IDs only; no path, link or HTML from text; no Go change.

## Files outside the task's list (needed for the click)
`agent/AgentPanel.tsx`, `App.tsx`, `pages/KnowledgeBasePage.tsx`, `agent/agent.css`, `agent/AnswerSources.tsx` (new, keeps `AgentCards.tsx` diff small because TASK-234 edits it).

## Decisions to confirm
- The locator inside the file (page N) is not opened; the file is marked and scrolled to, as for search hits (TASK-155).
- The card still shows no answer text or claims (TASK-118/120 follow-up): only the status line and the sources.

## Follow-ups
- Core call note id -> lecture to replace the frontend scan (up to 102 calls in the worst case); then drop `findNoteLecture`.
- Render claims, quotes and «не нашлось в лекциях» on the card (binding already parses them).
- Overlay edge: a transcript/summary overlay opened over «База знаний» during a running note lookup lets the late result open the lesson behind it (rare, harmless).
- Wiring (`App` / `AgentPanel` / `leaveJump` calls) has no DOM test; the owner checks by hand (see the QA file).
- `Card.Removed` checks the note only, not a replaced material (TASK-153 follow-up, Go).

## Changed existing tests
None. New: `test/answersources296.test.mjs` (parsing, labels, rendering, escaping, opening, jump ticket, note scan).

## Verification
Frontend: `npm run test` 1010 pass, `npm run build` ok (after merging master).

## Security
Auditor: none found (text nodes only, ids only, no new side effect, scan capped). Reviewer: approve; mediums (late jump overriding navigation, brittle wiring regexes) fixed and re-reviewed with approve; lows fixed except the overlay edge and the hover tint (same as `ag-asg-row`).
