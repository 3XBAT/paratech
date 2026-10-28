# TASK-337 — deadline without seconds, no «Ответ» heading, inline thinking indicator

## Done
- `homeworkView.dueText` uses `formatTimeMinutes`: «до 27.10.2026, 15:50» in the assignment list, page and summary meta. «Сегодня», agent assignment rows and the confirm-card «Срок» never showed seconds (covered by a test).
- Agent `reply` card has no caption (`replyCaption` removed). Captions of other cards («Ответ по материалам» etc.) stay.
- Waiting state: `div.ag-think` with the dots, a hidden «Агент отвечает…» in `role="status"` and a compact «Стоп»; the full-width `ag-card-wait` card and its CSS are gone. Dots are static under prefers-reduced-motion.

## Decisions to confirm
- `role="status"` (implicitly polite) instead of an explicit `aria-live="polite"`: `agentplan318.test.mjs` asserts the executing-plan markup has no `aria-live`.
- The plain reply card keeps its box; dropping the box too would be a trivial follow-up if the owner wants the pure Claude/ChatGPT look.
- Other seconds-showing timestamps are not deadlines (recordings, journal, summary update time, model install time, version createdAt, quota) and were left alone.
- `workView`/`turnkeyView` `answerLabel: "Ответ"` belong to the work/turnkey forms, not the agent reply.

## Follow-ups (reviewer lows)
- Narrow the `agentplan318` assertion to the `ag-plan-prog` element so an explicit `aria-live` can be used on the waiting row.
- The pending state before the first update shows dots without «Стоп» (pre-existing; nothing to stop yet).
- If a screen reader stays silent on the waiting row, mount an empty `role="status"` first and fill the text on the next tick.
- Test overlap: the thinking-row tests in `agent.test.mjs` and `panelfixes337.test.mjs`, and the source-text check on `homeworkView.ts`, could be trimmed.

## Changed existing tests
- `test/agent.test.mjs` «waiting: three dots…»: asserts «Агент отвечает…» instead of «Запрос выполняется» (text changed by the task).

## Verification
`npm run test` 1106/1106, `npm run build` ok (via heavy.sh). The frontend uses `node:test`, not vitest.

## Security
security-auditor: no findings (text still rendered as React text nodes; Stop wiring unchanged). Reviewer: approve, lows only.
