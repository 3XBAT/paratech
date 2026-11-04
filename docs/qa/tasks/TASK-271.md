# TASK-271 — check steps («Форк»)

Start from a cold app, agent panel open, a chat with at least three requests (the agent answered).

1. Under each of your messages there is a quiet counter-clockwise-arrow icon; hover: «Начать заново отсюда». Click it under the 2nd message.
2. A dialog opens: «Скрыть» (accent) with «Сообщения после этого агент забудет. Сделанные действия не отменяются.», «Форк» with «Будет создан новый чат с историей до этого сообщения. Этот чат не изменится.», and «Отмена» (focused; Esc also closes).
3. Click «Форк»: the button shows «Создаётся…» for a moment, then the dialog closes and a NEW chat opens, titled «<название> (fork)» (the « (fork)» part stays visible even for a long name). The thread shows the 1st request and its answer; the field holds the text of the 2nd message with the caret at the end; nothing is sent.
4. Under the chat title a quiet line: «Открыт новый чат. Исходный «<название>» не изменился.» with a small back icon (tooltip «Открыть исходный чат»). Click it: the original chat opens with all its messages, unchanged; in the chat list both chats are present.
5. Back in the fork send the message: the agent answers and knows the 1st request (it remembers the earlier talk), but not the later ones of the original chat.
6. While a request runs in a chat: «Форк» is available, «Скрыть» is off with a reason.
7. Typed text in the field before «Форк»: it stays, the message text is added after an empty line. After «Скрыть» + editing the text, then «Форк» on it: the edited text goes to the new chat.
8. Hidden messages: in a chat where you used «Скрыть» on some message, fork a later message: the hidden messages do not appear in the fork.
9. Long chat (more than 20 requests): the fork's context ring is the same level as the source's would be at that message; the agent in the fork still knows the beginning.
10. Refusals (dialog stays open, says why): chat gone — «Этот чат уже недоступен.» + «Новый чат»; message gone — «Этого сообщения уже нет в чате: оно скрыто или изменилось. Чат обновлён.» + «Новый чат»; failure — «Не удалось создать новый чат. Можно повторить.» (the «Форк» button is the retry).
11. Check on screen: the note and icon look quiet in dark and light theme, the tooltip is not clipped at the panel edge, forced-colors mode shows a border around the dialog.
