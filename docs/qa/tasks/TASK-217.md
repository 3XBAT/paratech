# TASK-217 — QA

Prerequisite: the Hub with catalog 6 (TASK-216) is deployed before this desktop build.

The panel rendering of the new line is TASK-220; until then check through the journal and the data.

1. In the panel: «завтра в 9 скинуть проверочную Иванову». Expected: an assignment is added with the due date tomorrow 09:00, no e-mail or send proposal appears.
2. «была пара по матанализу, нужно решить номер 5 к 30 октября». Expected: an assignment in the subject, due 30 October.
3. Undo the action right away: the assignment is gone.
4. Add an assignment, edit it by hand, then Undo: the undo is refused («Изменено после»), the assignment stays.
5. Ask for two assignments in one request: a plan is shown first.
