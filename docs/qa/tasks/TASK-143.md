# TASK-143: check steps

1. Send a request that takes a while. While it waits, a quiet «Стоп» is next to the waiting line (also reachable with Tab). Press it: it becomes inactive, then the card «Остановлено · Продолжить · Отмена» appears.
2. Press «Продолжить»: the stop card disappears while the work goes on.
3. Under the last finished request there is a round arrow («Запустить просьбу заново»). Press it: a new request with the same text appears below, and the arrow moves to it. Older requests have no arrow.
4. Rerun of a request that already changed data: the text «Повтор недоступен: просьба уже изменила данные. Можно отменить действия или задать вопрос заново.» appears.
5. Send a second request while the first still runs: the refusal «Предыдущая просьба ещё выполняется» now has «Стоп» and «Отправить ещё раз». «Стоп» stops the first; «Отправить ещё раз» sends the same text anew.
6. Double click on «Стоп» or the arrow: one action only.
