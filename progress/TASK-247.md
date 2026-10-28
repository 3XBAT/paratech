# TASK-247 — @-mentions in the agent input and pills in messages

## Done
- «@» at the start or after whitespace opens a list above/below the input (combobox + listbox, `aria-activedescendant`): arrows wrap, Enter/Tab/click pick, Esc closes only the list. Debounce 250 ms, one call in flight, stale answers dropped, «Поиск…» only after 600 ms, previous options/empty/unavailable line kept meanwhile.
- A pick removes the `@query` from the text and adds a removable pill in a row above the field (≤ 8, no duplicates, Backspace on an EMPTY field removes the last one, always-mounted `role=status` announces «Указано/Убрано»). An «@» button in the field keeps the feature visible after the first message; the empty chat hint mentions «@».
- Ask carries `{kind, id, label}` only; the pills row clears after a send.
- Pills in the thread (live requests, earlier requests, requests restored after a restart through `chatEarlier`) always show the stored label, as text nodes (title/aria attributes only), no «удалено». Click: subject → Предметы with it, assignment → Задания, lesson → «База знаний», material → «Предметы»; tooltip and aria say which page opens. A pill that leads nowhere is a plain span.
- New: `src/agentMentions.ts` (pure logic), `src/agent/useMentionComposer.ts`, `src/agent/MentionParts.tsx`; changed `AgentPanel(View).tsx`, `AgentCards.tsx`, `agent.css`, `agentView.ts`, a small `App.tsx` callback. Tests: `test/agentmentionsui.test.mjs` (≈ 50).

## Decisions to confirm
- A pick deletes the typed «@физ» from the sentence, so «сравни @физ и @хим» is sent as «сравни и» + two pills (Slack/Notion/Cursor keep the mention inline). Keeping the typed word without «@», or inserting the label, needs a product/ADR call (the label must not go to the Hub).
- Lesson and material pills open only the generic page (no route by id).

## Follow-ups
- Routes by id: lesson → its recording/summary, material → its subject page (store the subject id with the mention).
- No runtime test of `useMentionComposer` (no DOM test harness); the pure parts are tested.
- Live entries without a local scope and reruns of restored entries show pills only after the history reloads (low).
- Same-text announcements («Убрано: X» twice in a row) are not re-read (low).
- UX audit 15.1/15.3/15.8 (example chips, field moving, «запрос»/«просьба») untouched.

## Changed existing tests
- `agentchatsui.test.mjs`: empty lesson label fallback «Занятие» → «Пара» (app glossary).
- `work.test.mjs`: entry model literal gained `mentions: []`.

## Review
Reviewer: round 1 changes requested (3 medium: dead-end Enter, 8-limit text, hover contrast), UX expert «доработать» (6 must-fix) — all fixed; re-reviews of both fix rounds approved after one more a11y fix (second failure announcement). Security: no findings.
