# TASK-348 — боковая панель с подписями, «Записи» отдельным разделом, «Записать пару» на «Сегодня»

## Done
Frontend only (no Go, no new dependency), pure view state.
- Rail: icon + label under it, 72 px, seven items: top «Сегодня, Записи, Предметы, Задания, Расписание», bottom «Журнал, Настройки». The tooltip is gone (the label is visible; `aria-label`/`aria-current` kept). Label 11px, nowrap, ellipsis; focus ring drawn inside the button (`outline-offset: -2px`). The 64 -> 72 px width changed in styles.css, agent.css, `panelWidth.ts`, toast host.
- New page `recordings` («Записи») = the existing RecordingsPage. «Сегодня» lost the `all` mode, the «Записи» head button and «К сегодняшнему дню». «Проблема с записью» chips open «Записи».
- «Записать пару» (the only primary of «Сегодня»): one-shot state `startPanel` in App (like `scheduleReview`, not in the Route, `keepStartPanel` in `recordStartView.ts`); «Записи» opens with the start panel open and focus inside. While a recording runs the button reads «● Идёт запись 00:41 · Остановить» and stops it with `activity.stopRecording` (same binding as the rows). Start in progress (STARTING): disabled «Запись начинается…». A failed stop: «Не удалось остановить запись — она продолжается.» with «Повторить» and «Перейти к записям»; «Запись уже остановлена.» for not-running/not-found.
- Status line of «Сегодня»: «В очереди: N» (glossary: no «задачи»; the count mixes recordings and jobs) is a quiet button to «Записи».
- Speech-component install button is secondary on «Сегодня» and «Записи» (`primary={false}`), primary on the models page.
- While the start or import panel is open, the head «Начать запись» of «Записи» is not the accent (one accent).
- `RemoveSubjectDialog`: Windows button order, «Отмена» initially focused.
- Agent panel context «Записи» (label from the page); the screen sent to the core is `today` (see follow-ups).
- «Звук обрабатывается только на этом компьютере»: stays only in the empty state of «Записи» (TASK-347); no status-bar copy. «раздел «Записи»» texts (popup, source picker) are now true.

## Decisions to confirm
- `recordings` maps to the core screen `today` in `agentView.ts` (the Go core has no `recordings` screen and refuses unknown ones).
- Queue wording «В очереди: N» (no noun).
- Elapsed time `mm:ss` (`h:mm:ss` from an hour) on «Сегодня»; the rows of «Записи» still use `h:mm:ss`.
- Files outside the task list: `agent/agent.css`, `agent/panelWidth.ts` (rail width), `pages/SpeechComponentNotice.tsx`, `pages/SubjectRemoval.tsx` (allowed extra), `recordingRowView.ts` (`queueLine`), `activityView.ts` (only `export` of `errorMessage`), `pages/today.css`, `tsconfig.test.json` (one line: new module in the test build).

## Follow-ups
- Go: add a `recordings` screen to `values.Screen` so the agent gets its own context there.
- One duration formatter (`clockText` vs `formatDuration`).
- `statusPresentation.queueText` still says «В очереди N задачи» (check whether it is shown anywhere).
- Count of the queue differs: «Сегодня» counts recordings only, «Записи» also jobs (TASK-349 busy pill).
- Rail on very short windows has no overflow handling; verify «Расписание» fits at 100-200 % on the real app.
- Head «Начать запись» and the panel button share a name while the panel is open (consider «Скрыть»).
- Running-state recolor of the button (not the accent), focus on the panel's «Начать запись» on arrival: not done.
- Stale comments «Записи и задачи» remain in `TranscriptLinks.tsx`, `sourcePicker/SourcePicker.tsx`, `sourcePicker/advice.ts`.
- After deleting a recording from its summary opened over «Записи», `onDeleted` goes to «Сегодня».

## Changed existing tests
`nav.test.mjs` (seven items, labels instead of tooltip, bottom group), `panelwidth.test.mjs` (rail 72), `assignmentdone316.test.mjs` (toast `left: 72px`), `activity.test.mjs` and `today.test.mjs` (no `all` mode, `onOpenRecordings`, notice `primary={false}`), `recordingrow.test.mjs` (`queueLine` «В очереди: N»), `quiet347.test.mjs` (head toggle variant regex: primary unless a panel is open; the guard against the word `secondary` kept). New: `rail348.test.mjs`; `subjectremoval289.test.mjs` extended.

## Review
Reviewer: approve with lows -> after UX fixes a blocker (wait label «останавливается» guessed by age) -> fixed, plus retry button, import-panel accent, not-running wording; the last fix commit was not re-reviewed (small, tested). ux-expert: «доработать» (focus ring, stop failure text, two accents on «Записи», label fit, wait state) -> all fixed; not re-run. Security-auditor: not run — view-only, no binding or trust boundary touched.
