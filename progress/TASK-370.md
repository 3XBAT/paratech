# TASK-370 — «Подключиться» in the recording popup

Owner request 2026-11-03: the popup before a lesson shows «Подключиться» when the lesson's subject has a saved call link (TASK-364). The link is never shown; the meeting opens through the existing `LibraryService.OpenSubjectCallLink(subjectID)` (the page opens no URL).

## Done
- Go: `PromptService.CallSubject(id) -> {subjectId}` (`input/wails/prompt.go`). `recordingprompt.Service.CallSubject` (`cases/recordingprompt/calllink.go`) answers a non-empty id only for the CURRENT, OFFERED lesson prompt whose lesson subject string resolves (`subjectmatch`, Matched) to one subject that has a link. Everything else (ad-hoc, other id, lesson gone, unknown/ambiguous, no link, read errors, ports not wired) is `""`, never an error; failures are logged once as a result code, no cause, no link.
- Own consumer-side ports in `recordingprompt` (`Lessons.Get`, `SubjectResolver`, `SubjectCallLinks`; `Deps.Subjects`/`CallLinks` optional/nil-safe); `app/prompts.go` passes the library service for both (ARCH-50: app only wires; a `library` field in `core`). Differs from the draft hint of one `CallTargets` port in `app`.
- Frontend: `src/popup/joinView.ts` (pure `JoinController`: hidden/ready/opening/opened), thin wiring in `Popup.tsx`, `bindings/prompt.ts` (`callSubject`, strict parser), one CSS rule. Read when the offered lesson id changes, on `shownEvent` and when the page becomes visible; the newest reply wins; a late open reply after close is dropped; two quick clicks open once.
- States: opening «Открытие…»; opened -> grey disabled button (hover «Ссылка уже открыта») plus a visible quiet note «Ссылка открыта. Запись пока не идёт.» until the popup closes (page hidden / prompt id change); failure -> amber line «Не удалось открыть ссылку. Можно повторить.» last in the texts area (no window growth), button active again; no_link/not_found -> button gone, muted «Ссылки на пару уже нет.».
- Layout: «Начать запись» full width on top; «Подключиться» (secondary) and «Не записывать» side by side below (fit in 300 px, ux-expert). Without a join button the layout is unchanged.

## Decisions to confirm
- Placement: the first version stacked join ABOVE start (the owner text says «перед»); ux-expert showed it costs ~33 px of the 300 px popup and cuts the advice, so join sits next to «Не записывать» under the primary button.
- «Until the popup closes» = the page gets `visibilitychange: hidden` or the prompt changes. `shownEvent` alone does not re-enable the button.
- Opened state is not re-clickable (owner's rule); ux-expert suggested «Открыть ещё раз» instead — left as the owner said.

## Follow-ups
- Join button in the running-recording view (ux-expert: a student who clicks «Начать запись» first loses the way into the call from the popup) — product/scope decision.
- Offer state shows the red pulsing dot (same as «Идёт запись»); consider a neutral dot while only offered.
- a11y: the failure line has no live-region role (`role="status"`); the confirmation `role="status"` appears together with its text (some readers will not announce it).
- The button appears a moment after the popup (async read) and can shift the text area by one row.
- ADR-0032 has no addendum for the popup (recordingprompt now resolves lesson -> subject -> link through its own ports); add one line.
- Visual check of the 300 px fit (browser warning + advice + failure line, 125% text) was not possible without running the app.

## Changed existing tests
`prompt.test.mjs` (methods 7 -> 8), `iconbuttons368b.test.mjs` (word-button list gains `{callLinkTexts.join}`), `callLink364ui.test.mjs` ("popup untouched" now allows exactly `callLinkTexts`, `callLinkView`, `openSubjectCallLink`, still forbids the flow and set/clear/create), `TestPromptServiceMethodSet`, `TestFrontendCallsBoundNames` (+CallSubject), fakes (`fakeLessons.Get`, `fakePrompt.CallSubject`).

## Review
Reviewer (opus): approve with mediums/lows -> fixed (OFFERED check, refresh on visible) -> re-review approve. ux-expert (opus): «доработать» (grey dead end, 3 stacked buttons, failure banner, gone note) -> applied except the re-clickable button, join in the recording view and the dot colour (above). security-auditor: no findings.
Security: no link/host reaches the page or logs; the page sends only a subject id to the unchanged TASK-364 open path.
