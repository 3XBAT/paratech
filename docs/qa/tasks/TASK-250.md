# TASK-250 — check steps

Nothing changes on screen for the student: this is a background cleanup. Start from a cold app; the agent panel looks as before.

1. Open an old chat (a copy of a database with a chat whose requests are older than 90 days, or run the automated check: `go test ./desktop/tests -run AgentChatAfterTheDetailsPurge`).
2. Start the app. The log has the line «agent history details past 90 days» with the number of requests cleaned (only when there were old ones); once a day later the same step runs.
3. Open the old chat: all your messages and the agent's answers are still there (answers of cleaned requests may lose line breaks, see the progress file).
4. Send a new request in that chat: the agent answers and still knows the earlier conversation; the context ring is the same as before the cleanup.
5. A chat's last request (even months old) still offers «Повторить» / «Продолжить» as before; the action journal and «Отменить» of old actions are unchanged.
6. In the database `agent_steps` has no `calls`/`results` rows for requests older than 90 days (except the last request of each chat); `student` rows remain.
