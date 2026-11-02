# TASK-340 — the reply is typed out as it is generated (check steps)

Prerequisite: the Hub with `AgentStepStream` is running (`HUB_PROVIDER_STREAM` on, the default) and the desktop is the new build. A Hub without the new RPC still works: the reply just appears whole.

1. Start the app cold, open the agent panel (Ctrl+K / the chat button). You see the input and the hint chips as before.
2. Ask something with a long answer, for example «Объясни подробно, как работает TCP и чем он отличается от UDP». You see your bubble and the three waiting dots with «Стоп».
3. The first words of the answer appear: the dots disappear, the text grows in a card like the final answer, with a thin grey cursor at the end. «Стоп» stays under the text. If you scroll up while it types, the panel does not jump back, and «Стоп» stays at the bottom of the panel.
4. The answer finishes: the card has the same look (no jump), the cursor is gone; if you were at the bottom, the panel scrolled to the end.
5. Ask again and press «Стоп» in the middle: the typed text stays on screen, muted, with «Ответ остановлен», and under it «Остановлено» with «Продолжить». Press «Продолжить»: the full answer comes (it is not paid again).
6. Switch off the network while the answer types (or stop the Hub): the text disappears and the line says «Ответ прервался…» / the offline reason with a button («Продолжить» when the connection is back). Nothing half-typed is saved to the history.
7. Windows Settings -> Accessibility -> Visual effects -> Animation effects off: the cursor does not blink.
8. Check on screen: the sticky «Стоп» row has the panel colour in the light and the dark theme; a long word wraps in the card; a screen reader says «Ответ печатается…» and then «Ответ готов».
9. Ask something that needs a tool (for example «Что у меня сегодня?»): there is no flash of a half sentence before the cards.
