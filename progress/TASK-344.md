# TASK-344 — one word per thing (glossary pass over the UI texts)

## Done
- Frontend texts only, no Go. New `src/statusWords.ts` (waiting words: «Ждёт интернет», «Ждёт пополнения минут», «Ждёт зарядки», «Ждёт повтора»; `waitingInline`) used by activityView, journalView, recordingRowView, homeworkView, ticketsView.
- Glossary applied everywhere in `src/` (except `bindings/`): Конспект (not «Кратко»), пара (not «занятие»), Расшифровка / модуль расшифровки, Записи, Задания, «Добавить предмет» / «Новая папка» / «Исправить предмет или тип пары», «Написать черновик», «Адрес получателя заданий», turnkey caption + «Начать разбор» (agent card and «Задания»), «Ответ» caption removed, «Пропустить вопросы», «План», «Остановить», «Да, выполнить», «запрос», «Разобрать список», «Подбор остановлен», journal «Задание разобрано» / «Поиск заданий в записи пары». Owner terms kept: «Мой конспект», folder «ДЗ», «Контекст чата».
- One deadline wording (`src/dueText.ts`, moved out of todayView to break an import cycle; todayView re-exports): agent «added» line («завтра до 18:30»), agent overdue row keeps the date, «Задания» (`dueText(due, now, done)`: open -> «завтра до 18:30» / «просрочено, …», done -> plain «до …»; «Срок не распознан»; year shown only when it differs from now). The lesson summary keeps the plain «до …» (no `done` in its binding).
- Review round: «Не выполнять» on the plan card, «Отменить запрос» on stop cards, quota stop text no longer promises auto-continue, one «Повторить» per request, impersonal status texts («Проверка…», «Сохранение…», «Запуск…» …), «Написать новую версию», «Черновик пишется…», «План работы готов», sr-name «Завершённые · фоновая обработка», «Идут» is an h2, ticket hint.
- `test/words344.test.mjs`: waiting dictionary, wording, deadline cases and a source scan for old words (bindings excluded, the Windows setting «Распознавание текста» allow-listed).

## Decisions to confirm
- «Ждёт повтора» added to the waiting dictionary (not in the glossary); the recording-row «повтор позже» now reads «В очереди · ждёт повтора».
- Beyond the AC, glossary "do not use" forms applied: «Задачи» -> «Фоновая обработка» (RecordingsPage), «Сгенерировать заново» -> «Написать новую версию», «Запустить снова»/«Попробовать снова» -> «Повторить».
- Journal/activity job labels keep «Решение задания» (TASK-102 test restricts «черновик» to homework files).
- Turnkey caption kept as written in the AC («1–2 вопроса», «Разбор расходует минуты»); ux-expert says it is slightly inaccurate (up to 2 rounds; writing also costs minutes) — check billing and wording.
- Glossary additions suggested: «Начать разбор», «Написать новую версию», «Не выполнять», «Отменить запрос», «Ждёт повтора».
- Dependency: popup and source-picker texts say «в окне paratech, раздел «Записи»», but the «Записи» section appears in the rail only with TASK-348 (until then it opens from the «Записи» button on «Сегодня»). Land with/after TASK-348.

## Files outside the allowed list (string literals/comments only unless noted)
lessonLabel.ts, dueText.ts (new), pages/{KnowledgeParts,ModelsPage,TranscriptPage,SettingsPage,ConnectionsPage,HomeworkDialogs,AssignmentDoneParts}.tsx, agent/{TurnkeyCards,WorkCard}.tsx (the «Ответ» caption JSX removed), calendarView/diagnosticsView/googleView/quotaView (status texts), todayView.ts (words + dueText move; may conflict with TASK-345).

## Follow-ups
- Go-side texts with old words (shown as is; change Go and the `bindings/*.ts` mirror together): input/wails transcript.go, summary.go, speech.go, models.go, schedulepicker_windows.go, lecturetext.go, library.go («Занятие не найдено в расписании.»), knowledgebase.go, agentconfirm.go («…в «Домашних заданиях»», «…списка ДЗ»), activity.go (`draftLabel "ДЗ: "`), recordingdelete.go («Краткое содержание»); summaryexport headings; email subject «Домашнее задание…»; bindings/turnkey.ts «сгенерируйте раздел заново».
- Lesson page: tab «Конспект» and button «Открыть конспект» now share a name (layout, TASK-345…347).
- Subjects tree header: «Добавить предмет» + «Новая папка» are wide for the 220 px column and wrap (layout task).
- «Пропустить вопросы» does not say that defaults are taken; «Остановка…» vs «Останавливается…»; journal «Поиск заданий в записи пары» is a noun phrase among past-tense events (AC-mandated).
- workView blocked state: «Написать новую версию» actually continues from the first unwritten section (pre-existing mismatch with «заново»).
- Lesson summary assignments have no `done` flag, so deadlines there stay plain.
- «Сроки на неделе» chip («через 3 дня, до 18:30») is the last non-dueText pattern (TASK-345).

## Changed existing tests
Expected strings only, for ~40 test files (activity, advice, agent*, connections, diagnostics, followups193, homework*, knowledge, lesson, library, loadestimate, models, notelectures155, ownnotes, prompt, quota, recordingdelete, recordingimport, recordingrow, schedule, soundlevel, sourcepicker, status, subjectpage227, subjectsHome341, teachers238, tickets, today, turnkey, work). Semantic: agentassignments / agentassignmentadded (overdue row keeps the date, no «срок»), panelfixes337 («Задания» deadline format and `dueMoment(due, now, done)` source check; «Срок не распознан»), recordingrow (h1 «Записи», h2 «Фоновая обработка», no h2 «Записи», «Идут» h2).

## Review
Reviewer: changes requested -> fixed (deadline in «Задания», impersonal texts, two «Повторить», sr-name) -> re-review found a regression (summary «просрочено» for done) -> fixed. ux-expert: «доработать» (3 must-fix: «Задания» deadlines, «Не выполнять»/«Отменить запрос», quota text) -> fixed; items 8–10 left as follow-ups. Security-auditor: not run — no non-text logic, no trust boundary touched.
