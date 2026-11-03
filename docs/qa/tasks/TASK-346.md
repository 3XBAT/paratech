# TASK-346 — «Задания» and «Журнал» (check steps)

Start the app cold with your data, open «Задания».

1. The list head has one accent button «Добавить задание». With no assignments at all the page says «Задания находятся сами в записи пары после конспекта.» and offers «Добавить задание вручную».
2. Rows are under headings «Просрочено · На этой неделе · Позже · Без срока» with counts; empty groups are absent. With more than 8 rows in a group it has its own «Показать ещё». «Выполненные» is one flat list; in «Все» done ones are in the last group «Выполнено». Mark a row done with the keyboard in «Все»: the focus stays on that row's check.
3. Open an assignment: «Выполнено» is visible; «⋯» holds «Изменить», «Поправить предмет и преподавателя», «Удалить» (last). «Удалить» still asks first when a work is running; «Отмена» and «Вернуть» put the focus on «⋯».
4. In the draft card «Сохранить ▾» offers «Файл .txt» and «Word (.docx)»; with an unsaved edit it and «Отправить преподавателю…» are off with «Сначала сохраните или отмените правку.». Check the menu opens under the button and is not clipped.
5. «Письма»: «Отправить ещё раз…» is a normal button; a letter with unknown outcome shows one line about the mail not confirming delivery and «Письмо дошло».
6. «Журнал»: no «Обновить». Only «Период» and «Только отменяемые» are visible; «Ещё фильтры» opens the rest (shows a count when set). A line that cannot be undone says «Нельзя отменить: …» (sent email: «письмо уже отправлено»). «Отменить» on an undoable line: «Действие отменено.» appears in that row. Filters with no result: «По выбранным фильтрам ничего нет.» and «Сбросить фильтры».
7. With the network off (journal load error): the error has «Повторить» next to it.
