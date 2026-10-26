# TASK-201: back/forward navigation, subject in Today rows

## Done
- `src/navHistory.ts`: pure history (route list + position, depth 50, no duplicates, truncation on navigate after back), `handleKey` / `handleMouse` pure handlers.
- `src/useNavHistory.ts`: hook applying them. Alt+Left/Right (Alt alone; skipped in input/textarea/select/contenteditable, on defaultPrevented and on repeat); mouse buttons 3/4 cancelled on mousedown/mouseup/auxclick, navigation on mouseup only. Backspace unused.
- `App.tsx`: one hook call; restore callback remounts the page only when the page/assignment differs from the route being left (`needsRemount`).
- «Ждёт вашего «Да»» row: subject is its own muted line (`.today-subject`, 2-line clamp, hidden when empty); «Горит на 7 дней» uses the same class.
- Tests: `test/navhistory.test.mjs` (history, keys, mouse, remount), `today.test.mjs`.

## Decisions to confirm
- History entry = existing `Route` (page, transcript, summary, assignment); settings tabs are pages.
- Summary's own «Назад» also adds an entry (as in a browser).
- Only the subject is clamped to 1-2 lines; assignment titles still wrap.

## Follow-ups
- The open subject in «Предметы» (`SubjectsPage` openId) and the assignment opened from the «Задания» list (`HomeworkPage` openId) are component-local, so back skips them. Lifting them into `Route` touches both pages (TASK-195 edits the work card in parallel), deferred.
- Alt+Left inside a text field is not cancelled (WebView default applies).

## Changed existing tests
- `today.test.mjs` waitingLine: `meta` no longer contains the subject; `subject` asserted separately.

## Security
Auditor: no findings. History is in memory, only app-created Route values; subject rendered as text.
