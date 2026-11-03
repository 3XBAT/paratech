# TASK-371 — check steps (small buttons as icons)

Start from a cold app: the main window opens on «Сегодня».

1. Recordings: tick two or more rows. The bar shows «Выбрано N», a RED trash and a grey cross. Hover and Tab to the trash: tooltip «Удалить выбранные (N)». Press it: the same confirmation dialog as before; nothing is deleted without it.
2. A subject with a call link: the block shows the link, the word «Подключиться», a pencil and a red trash. Hover/Tab: «Изменить ссылку», «Удалить ссылку». After deleting, an undo icon «Вернуть ссылку» appears beside «Ссылка на пару удалена.» and takes the focus. «Добавить ссылку», «Сохранить», «Отмена» are still words.
3. «Сегодня», a pair with a link: a pencil next to the host (tooltip «Изменить»). If the link does not open: «Можно повторить» with a retry icon, «Подключиться» stays.
4. Chat: stop a request. The line reads «Остановлено. Можно продолжить.» with a play icon and a red cross (tooltips «Продолжить», «Отменить запрос»). On a failed step or lost connection the line says «Можно повторить» and shows a retry icon («Повторить»).
5. A confirm/schedule card that cannot be read: «Карточку не удалось загрузить. Ничего не выполнено. Можно повторить.» with the retry icon inline at the end.
6. Work card plan editor: up/down/remove icons of each row are named «Поднять раздел N» etc.; the remove icon of a fixed slide is disabled and explains why on hover.
7. Words that must stay: «Повторить» after a screen error, «Сделать конспект заново», «Удалить все», «Отменить всё» (see the decisions in the progress file), «Развернуть/Свернуть все», card-form «Сохранить/Отмена/Пропустить», the lesson popup (untouched).
8. Tooltips in the narrow chat panel are not clipped.
