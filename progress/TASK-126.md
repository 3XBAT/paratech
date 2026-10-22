# TASK-126: «Сегодня» — new main screen (frontend)

Done: bindings/today.ts (strict parsing), todayView.ts, pages/TodayPage.tsx + TodayParts.tsx + today.css, agentDraft.ts (chip click puts text into the agent panel input, never sends; one useEffect in AgentPanel). Sections: hint chips, Пары дня (live «Идёт запись»/«Распознавание · N %» from the activity binding, problem chip), Ждёт «Да» (rows open the assignment, no buttons), Горит на 7 дней, Готово за день, posts. The old recordings/tasks page stays behind «Записи и задания» (in place, with back button). Nav unchanged.

Review: round 1 changes requested (3 Medium: pending summary shown as done, stale today() replies, recording problems hidden) fixed + Lows; re-review approved. Security: no findings.

## Gaps (not faked)
- «Да, в календарь» absent: no path creates a create_deadline_event proposal from an assignment id (TASK-125 follow-up; TASK-127). post.calendar is parsed, not rendered.
- No confirm/reject buttons in «Ждёт «Да»» (no confirm call in the binding) — TASK-127/128.
- No refresh on agent:update; refresh on focus, stage change, «Обновить».
- Mock has the ask field on the page; here chips fill the panel input (one agent layer). Decision to confirm.
- SubjectPosts is wrapped but unused.

## Follow-ups (Low from re-review)
problemFor shows a failure chip even if a later recording of the lesson succeeded; dayProblems with unparsable day shows all failures; recordings poll has no newest-reply guard; same post appears in lesson chip, posts and Done.

## Changed existing tests
None. tsconfig.test.json lists the new modules.
