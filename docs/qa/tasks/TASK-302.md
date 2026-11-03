# TASK-302 — check steps (import panel, keyboard)

Start the app cold. Everything below with the keyboard only (Tab, Enter, Space).

1. «Записи» with an empty list: the student sees the empty state with «Импортировать запись». Tab to it, Enter. Focus lands on «Пара из расписания» in the panel (one jump, no second one). Tab to «Закрыть», Enter: the panel closes and focus is back on «Импортировать запись».
2. «Записи» with rows: the same from the button in the head. «Начать запись» and «Импортировать запись» have the same height side by side.
3. The same on a subject page, tab «Пары», and on «База знаний» -> a subject.
4. In «Пары» with the panel open, click «Добавить конспект прошедшей пары»: focus stays there, does not jump back.
5. Screen reader (if available): the opener announces expanded/collapsed.
6. Start an import of a small test audio file (not your real recordings): press Enter on «Выбрать файл и импортировать», pick a file. While it runs the button shows «Импорт…» dimmed, the mode switch, fields, «Закрыть» and «Импортировать запись» are dimmed (the opener shows «Недоступно, пока идёт импорт записи.»). After the native dialog closes, Tab goes on in the panel (focus did not jump to the top of the page). When done, everything is active again.
