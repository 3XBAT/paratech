# TASK-189 — free-work notice card UI

## Done
- `free_work` card rendered in the agent panel (`agent/FreeWorkCard.tsx`, logic in `freeWorkView.ts`, testable without a DOM). Text, «Сделать» / «Не надо», checkbox «Больше не показывать» (passed as `dontShowAgain` with both answers). Buttons and checkbox disabled while a call runs; single-flight (one `freeWork` call on a double click).
- On mount the card reads `freeWorkCard(ref)`: done -> work card, undone -> «Работа отменена», waiting -> buttons; unreadable -> error + «Обновить». After «Сделать» the work card shows (no plan card); after «Не надо» the line «Не делаем».
- failed/odd/throw from `freeWork` -> the card is re-read first (failed then done shows the work), else the known error text and the buttons return.
- Settings: group «Помощник», switch «Предупреждать перед свободной работой» (`freeWorkNotice` / `setFreeWorkNotice`).
- All text is React text nodes; the card never holds the topic (the core card carries only the call ref); errors come from a fixed whitelist.

## Decisions to confirm
- A declined card reads as waiting again when a thread is reopened (TASK-188: «Нет» stores nothing), so the acceptance line «повторный показ рисует итог» holds for done/undone only. A core change would be needed to persist the decline.

## Follow-ups
- The Go work DTO has `topic` and `subjectId` but no `sendable`; not added (Go untouched). WorkCard shows the topic as file name, the subject is not shown. Needed by the TASK-187 note; belongs to a Go task.
- A stored "failed" card still shows buttons (a repeat replays the failure per TASK-188); better «Обновить» only (reviewer low).
- Settings switch is covered at binding level only (no DOM in the suite); tests count `disabled` by substring (fragile) (reviewer lows).

## Scope beyond the file list
New `src/freeWorkView.ts`, `src/agent/FreeWorkCard.tsx`; one-line wiring in `AgentPanel.tsx`; `SettingsToggle` union in `bindings/settings.ts`.

## Changed existing tests
None.

## Security
Auditor: no findings. Reviewer: approve, no blockers; lows listed above.
