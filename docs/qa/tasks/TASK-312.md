# TASK-312 — check steps: subjects from the schedule

Cold start, a fresh profile: no subjects, no schedule. A schedule file with 8-10 lessons of 4-5 subjects is needed, incl. some spelled several ways («Матанализ», «Мат. анализ», «Матан. лекция»).

1. Open «Предметы». The student sees «Предметов пока нет», the text «Предметы создаются из расписания или вручную» and ONE accent button «Импортировать расписание»; «Добавить предмет» is secondary. Click the accent button: «Расписание» opens.
2. Import the file there (the usual preview, then confirm). After the import one quiet line appears: «Из расписания можно создать предметы: N» with a secondary «Завести предметы из расписания». Click it: «Предметы» opens with the review panel already open (focus is on the panel, not on a button).
3. In the panel: every row is ticked; the name is an editable field in the row; other spellings are small grey text; kinds read «лекции 8 · семинары 6»; a doubtful group is merged and says «Похоже, это один предмет…» with «Вместе · Отдельно · Не создавать» per doubtful member (Tab stops once on the group, arrows change the choice).
4. Untick one row: the button reads «Создать N предметов» with the new number. Untick all: the button is disabled and the reason «Отметьте хотя бы один предмет» is written next to it. Clear a name or type a name equal to another row: the error stands beside that field and the button is disabled with the reason.
5. Press Esc inside a name field: nothing closes. Press Esc outside a field: the panel closes and the list stays.
6. Press «Создать N предметов»: «Создание…» on the button, then «Создано предметов: N» with «Отменить всё». Close the panel: the line stays above the list with «Отменить всё» and «Закрыть».
7. Press «Отменить всё»: «Отменено: предметов N». Put a file into one created subject first (drop it on the window or «Добавить файл» and sort it) and undo again: that subject stays and the line «В «…» уже есть файл — оставлен» is shown.
8. Open «Предметы» again with subjects present and rows in the schedule still without a subject: above the list a quiet line «Из расписания можно создать предметы: N» + «Завести предметы из расписания»; the header keeps one accent («Предмет»). When everything is created the line is gone; opening the panel via the empty hint says «Все предметы из расписания уже заведены».
9. Refusals (can be seen with a stale file or by importing another schedule while the panel is open): «Расписание или предметы изменились» + «Обновить список» keeps the names typed before; busy/unavailable texts come with «Повторить»; every refusal has a button.
10. `npm --prefix <repo>/paratech/desktop/frontend run test` and `run build`: ok (test/scheduleSubjects312.test.mjs).
