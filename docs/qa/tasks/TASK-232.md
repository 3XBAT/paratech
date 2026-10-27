# TASK-232 — owner's check

1. `npm run test` and `npm run build` in `paratech/desktop/frontend` pass.
2. «Задания»: press «Удалить» on an extracted assignment and on your own one. Each becomes «Задание удалено» with «Вернуть»; press «Вернуть», both are back.
3. Delete one, open «Сегодня» and «Горит на 7 дней»: it is not there.
4. Delete an assignment that is being sent or written: the text «Сначала дождитесь отправки или работы над заданием» appears.
5. Open an assignment, press «Изменить»: the form shows the current title, subject, date, time, note. Change the subject, set the deadline to tomorrow 09:00, edit the note, save. The change is visible in «Задания», on the subject page and in «Горит на 7 дней».
6. In the form: Enter saves, Esc cancels; an empty title shows the error at the field.
7. For an extracted assignment tied to a lesson the empty subject option is «Предмет занятия».
8. Rename a subject the assignment uses, open «Изменить»: an error at «Предмет», save is disabled until a subject is chosen.
9. Look at the dark theme/accent of the new buttons and the removed line, and focus after delete and restore.
