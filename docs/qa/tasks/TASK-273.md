# TASK-273 — owner's check

1. Ask the agent for something with a plan that needs «Да» (e.g. a calendar deadline), so the plan card with «Да» / «Изменить» / «Отмена» is shown. Do not press «Да».
2. Close the app, start it, open the panel and the same chat.
3. Expected: the request is a card with the plan and the buttons, not a text line. «Да» runs the plan; the card then updates in place (no second card).
4. A finished request (DONE, FAILED, CANCELLED) of a chat still shows as a text line only. A request stopped by «Стоп» shows «Продолжить».
5. Earlier requests of the chat stay text.

Not covered (open question, TASK-272): a confirm card (propose_*) of an ended request is not restored in the chat; it is on «Сегодня».
