# TASK-318 — long plan card (UI)

## Done
- `src/planView.ts` (new, pure): `planCollapseLimit` = 8, Russian plurals (`stepsWord`), `moreStepsLabel` («Ещё N шагов»), `approveLabel` («Да, выполнить N шагов»), `progressLabel` («Шаг k из N»), `planWindow(total, expanded, failedIndex)`, `stepMarks`, `failedStepIndex`, `planProgress`.
- `AgentCards.tsx`: exported `PlanSteps` (props `steps`, `marks?`, `expanded`, `onToggle`); `PlanCard` keeps the expanded flag and shows the «Шаг k из N» indicator in the header row. More than 8 steps: first 8 plus a muted text button «Ещё N шагов» / «Свернуть» (`aria-expanded`, `aria-controls`); the opened list scrolls inside the card (max-height 18rem, focusable region «Шаги плана»). 8 steps or fewer: markup as before. The cost of paid steps stays one line (`notes`). One «Да» for the whole plan, «Изменить» and «Отмена» as before.
- `agentView.ts`: plan `CardModel` keeps `steps`, `caption`, `summary`, `notes`, `buttons`; gains `total`, `hidden`, `marks`, `progress`; the approve label is overridden there (`buttonLabels.approve` stays «Да»).
- `bindings/agent.ts`: optional `PlanStepView.state?: "" | "done" | "failed"`, parsed leniently (absent stays absent; anything else is pending; never throws).
- `agent.css`: `.ag-plan-*` rules; failed uses the existing `--rec-t`, the running dot is the existing `.ag-dot` (reduced motion already handled).
- No Go change: `input/wails/agentpanel.go` `planView` has no step cap (the only cap is the domain `values.MaxPlanSteps` = 8, TASK-317).
- Tests: `test/agentplan318.test.mjs` (16: boundaries 0/1/3/8/9/30, plurals, approve label, progress, lenient parse, SSR at 3/8/30 collapsed and opened, failed step beyond 8, progress and markers, injection, no `innerHTML`).

## Decisions to confirm
- «Шаг k из N» is a plain muted span, no live region (a live region would announce every step).
- A failed step at index 8 or later opens the whole list and removes the toggle (no dead «Свернуть»).
- Markers and the indicator appear only when the core sends at least one step `state`; k is never guessed from other cards.
- The failed step's way out stays the stop card under the plan («Повторить»); no second button inside the plan card.

## Follow-ups
- (Reviewer medium, deferred; needs Go) The core sends no step states, no per-step failure reason and no «Продолжить» on the plan card, so in the real app the indicator, markers and «упавший — с причиной и «Продолжить»» do not appear yet. A Go task (step-state feed from `thread` `used` flags through `AgentUpdate` / `AgentPlanStepView`, wave-11 plan runner, ADR-0033 §4) must add `state` (fix the exact value list, incl. "running"), an optional `reason` per failed step shown as a muted line, and «Продолжить» when the request stops on a failed step.
- Plans over 8 steps are reachable only after TASK-317 (domain cap) and the Hub schema; until then the 30-step case is covered by tests.
- Lows: with a collapsed list a running step at index 8+ is hidden (treat the current index like `failedIndex` in `planWindow`); `total` and `hidden` of the plan model are not read by the renderer (`hidden` ignores a failure reveal) — remove or use.

## Changed existing tests
`test/agent.test.mjs` "plan card: caption…": the buttons of a 3-step plan are now `["Да, выполнить 3 шага", "Изменить", "Отмена"]` and `>Да, выполнить 3 шага<` (the new acceptance criterion); it also asserts the first button id is still `approve`. Not weakened.

## Security
Auditor: no findings (model text only as React text nodes; `state` is absent from the Go view struct so the model cannot fake it; N in the button is the count of parsed steps). Reviewer: approve, no blocker/high.
