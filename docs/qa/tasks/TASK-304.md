# TASK-304 — a deadline without a time is 20:00 (check steps)

1. Start the app cold, open «Задания» and press «Добавить задание». Under «Время» the hint reads «Необязательно. Если время не указано, срок — 20:00.»
2. Enter a title and a date, leave the time empty, save. In «Задания» the deadline is that day, 20:00.
3. Open «Изменить» on it: the time field is empty. Save without changes: the deadline stays 20:00.
4. Add one more with the time 23:59 and open «Изменить»: the field shows 23:59 and saving keeps 23:59.
5. Ask the agent «запиши задание к пятнице»: the line «Задание добавлено: … срок пт, … 20:00».
6. Ask the agent to move the deadline of an assignment without one to a day: it becomes 20:00; with a deadline the time of day stays.
7. Old assignments at 23:59 (also extracted ones) are not rewritten; they show 23:59.
8. Known gap: «Сегодня» and the agent's assignment rows show only the date, no time (follow-up, see progress).
