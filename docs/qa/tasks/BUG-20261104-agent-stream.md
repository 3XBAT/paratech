# BUG-20261104-agent-stream — check steps

Start the app cold and open the side panel «paratech». First you see the empty hint «Можно задать вопрос или попросить что-нибудь сделать.» and the request field. (A new Hub build is not needed; the Hub is not restarted.)

1. Long reply (the bug). Ask for something long (a detailed summary of a lecture). Watch the reply type out.
   - Let it run past 2 minutes if the reply is that long: it must not be cut at 2 minutes while text keeps arriving.
   - Break the network (or stop the Hub) while it types and wait about a minute. The printed text must stay (muted), with one line «Ответ оборван. Можно повторить.» and a retry icon (tooltip «Повторить»). No second «Повторить».
   - Press the retry icon: the kept block is replaced by the new run.
   - Press the stop icon during typing: the text stays with the caption «Ответ остановлен» and the normal stop line; continue replaces it.
   - If it breaks before any text appeared, you see only the normal line «Не завершено: нет связи.» and the retry icon.
2. Timer. Send a request: next to the dots a quiet grey «1с», «2с» … «59с», then «1м 5с». Nothing for the first second. While text types, it stays in the row with the stop icon. After a stop, a failure or an answer it disappears; «Продолжить»/«Повторить» count from 0 again. A screen reader reads «Агент отвечает…» once, not the seconds.
3. Queued label. While a request runs, send a second one in the same chat. Today the core refuses it as busy (line «Предыдущий запрос ещё выполняется…» with stop and resend icons); the row «ждёт предыдущий запрос» with a stop icon can flash for a moment before it. Report what you see; whether the core should really queue is an open decision.
