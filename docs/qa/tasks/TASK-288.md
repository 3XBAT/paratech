# TASK-288 — subject removal core (check steps)

The student sees nothing new yet: the «Удалить предмет» button with the warning and «Вернуть» comes with TASK-289. This task is the core under it.

1. Start the app cold with your existing data (the migration adds one empty column). All subjects, files and notes look exactly as before; nothing disappears.
2. Removing a subject that has notes from the agent's Undo of a just-created subject still refuses (your notes are never taken).
3. Automated check (the owner does not need to run it): `go test -race ./desktop/tests/ -run SubjectRemoval` hides a subject with notes and files everywhere (library, notes, knowledge base, search, subject matching), shows everything again after restore, and refuses restore after 3 days.
