# TASK-249 — «Начать заново отсюда» → «Скрыть» (check steps)

The «Форк» choice is not built yet (TASK-271 binding); the dialog has «Скрыть» and «Отмена» only.

1. Start the app cold (dark theme) and open a chat that has two or three finished requests. The panel shows «Ранее в этом чате» and the thread. Under each of your messages a quiet, muted «Начать заново отсюда» is visible; it becomes full on hover and on keyboard focus (Tab reaches it).
2. Press it. A window «Начать заново отсюда?» opens over the panel: «Сообщения после этого агент забудет. Сделанные действия не отменяются.», buttons «Скрыть» (the one accent) and «Отмена». Focus is on «Отмена»; Tab stays inside; Esc, «Отмена» and a click outside close it with nothing changed and focus returns to the button.
3. Press «Скрыть». The window closes, the text of that message (with its @-pills) is in the input, above the field a note «Правка сообщения. После отправки всё, что ниже него, пропадёт из чата.» with «Отменить». «Отменить» returns the input to what it held before.
4. Press the action again, change the text, send. The messages from the chosen one onward disappear and the new request is shown; they stay hidden after reopening the chat or restarting the app. Actions in the journal stay. The agent still remembers everything before the chosen message (also what an earlier restart of an expired request had cut off).
5. Start a request that takes a while and press the action on an earlier message. «Скрыть» is disabled with «Агент ещё работает над просьбой…» and a button «Остановить»; after the stop it becomes available. For a plan waiting for «Да» the way out is «Отменить просьбу».
6. Switch to another chat or press «Новый чат» while the edit is pending: the note disappears and the input returns to what it held before. Inserting text from another page («спросить об этом») also ends the pending edit.
7. With the Hub off or no key: the window says there is no connection and offers only «Отмена». If a send is refused (for example the agent got busy), the note shows the reason; if you typed new text meanwhile, «Вернуть текст сообщения» puts the sent text back.
8. On an expired request the card button now reads «Запустить снова» (it was «Начать заново»).
