# TASK-398: check steps (the waiting row itself appears with TASK-399)

Start from a cold app with the Hub set up. Until TASK-399 the panel does not draw the waiting row, so this task is checked by behaviour and logs.

1. Open the agent panel, ask a long question («Составь план на неделю по всем предметам»). While it is being answered, ask a second one in the same chat. Expected: the input is not blocked and no «занято» error; after the first answer finishes, the second one starts by itself and gets its own answer.
2. Repeat while the agent runs a tool (e.g. «Добавь задание…» then at once another question): the second question waits and starts after the first request ends.
3. Press «Стоп» on the first answer while a second question waits: the first stops, the second starts afterwards (the stop does not cancel it).
4. Ask six questions quickly in one chat: the sixth gets the old «занято» refusal; the first five are answered in order.
5. Close the app while a question is waiting and open it again: the waiting question is not sent (nothing charged).
6. Hub cut: with a long answer in progress, if the stream breaks after it had started, the request ends as «Не завершено» with «Повторить» (failed), not «Нет связи».
