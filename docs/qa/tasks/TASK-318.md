# TASK-318 — QA

Plans over 8 steps are not reachable in the app yet (domain cap, TASK-317), and the core sends no step states yet, so the
long list and the progress markers are checked by tests; the screen check covers the short plan and the look.

1. In `paratech/desktop/frontend`: `npm run test` and `npm run build` pass (`test/agentplan318.test.mjs`: 3, 8 and 30 steps, progress, injection).
2. Cold start: open the app, then the agent panel (Ctrl+K). The student sees the empty hint «Можно задать вопрос или попросить что-нибудь сделать.» Ask for something that needs a plan (e.g. create a subject and add a deadline).
3. The card «Понял так» lists the steps numbered, with the cost in one line under them if any step is paid. The buttons read «Да, выполнить N шагов» (the accent one), «Изменить», «Отмена». With 8 steps or fewer there is no «Ещё» button and no inner scrollbar.
4. «Изменить» opens the wish field; «Отмена» closes the plan. «Да, выполнить N шагов» runs the plan; the card then has no buttons.
5. Tab from the buttons: no hidden stop. (Long plan, once TASK-317 lands: «Ещё N шагов» opens the rest, the list scrolls inside the card while the panel stays put, Tab reaches the list and the toggle with a visible focus ring, «Свернуть» closes it.)
6. Dark theme and Windows animations off: no new colours besides the existing red of an error; the running dot (once the core sends states) does not pulse.
7. «Шаг k из N» and the ticks appear only after the core sends step states (follow-up in `progress/TASK-318.md`); now the executing plan card is the plain list.
