# TASK-286 — check steps

No visible change in the UI: the work is in the logs.

1. Start the app cold; the library page opens with the subjects as before.
2. Do something that fails on purpose, e.g. discard a subject that was already removed in another window, or open the page of a missing subject. The page shows the same Russian message as before.
3. Open `<DataDir>/logs/desktop.log`. The line for that operation holds a `result` and, for storage problems, `cause=local storage` or `cause=unexpected`; no teacher name or e-mail, no file path, no wrapped database error.
4. Set a teacher on a subject and check the log: it never shows the teacher's name or e-mail.
