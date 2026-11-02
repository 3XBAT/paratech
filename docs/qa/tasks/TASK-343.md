# TASK-343 — deadline time everywhere (check steps)

Start the app cold with a few assignments: one due today at 20:00, one tomorrow at 18:30, one in three days, and one whose time has passed.

1. Open «Сегодня». The student sees the deadline rows: the chips read «сегодня до 20:00», «завтра до 18:30», «через 3 дня, до 18:30»; a passed one reads «просрочено». The nearest-deadline line reads «Ближайший срок — пт, 23 окт., 20:00» (passed ones are not counted). Lines under a lesson post and in the waiting list read «сегодня до 20:00» / «просрочено, …».
2. Ask the agent «какие у меня задания». In the reply list a deadline of today before its time reads «сегодня до 20:00», after the time «просрочено» (red). Tomorrow «завтра до 18:30», later «до сб, 24 окт., 18:30».
3. Make the agent propose a deadline for the calendar: the card row «Срок» reads «сб, 24 окт., 18:30».
4. «Задания» -> «Добавить задание»: the time field already shows 20:00, hint «По умолчанию — 20:00.». Leave it, save: the deadline is due 20:00.
5. Edit an assignment with a deadline: clear the date completely (also segment by segment) and save: the deadline is removed with no error. Type only the day segment into an empty date and save: «Дата указана неверно.», nothing is sent. Edit an assignment without a deadline and pick a date: the time fills with 20:00. Clear the date of an 18:30 deadline: «Укажите дату или очистите время, чтобы убрать срок.».
