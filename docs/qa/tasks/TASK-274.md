# TASK-274 — owner's check

Use a COPY of the data (the purge deletes for good). Do not run it on the real data until the checks pass.

1. Delete an assignment on the Homework page: the row «Задание удалено» with «Вернуть» appears; «Вернуть» brings it back.
2. Delete another one, then set the computer clock 3 days and a minute ahead (or edit its `deleted_at` in a copy of the DB), and start the app.
3. Expected: the assignment is gone after the start (no row, no draft, no work, no sent-mail record, its export files are gone); other assignments, live or deleted less than 3 days ago, are untouched.
4. An assignment whose email is still unresolved (sending / unknown outcome) is not purged; the log has a line about it.
5. «Вернуть» after the window answers «expired» (the screen still shows the generic failure; the text is TASK-275).
6. Leave the app running past midnight: the purge runs once a day, no visible effect.
