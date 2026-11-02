# TASK-344: One word per thing (glossary pass over the UI texts)

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** —
- **PRD sections / AC:** ADR-0034 §6, `docs/design/glossary.md`; audit А-0.4–0.7, 2.1, 3.3, 5.4, 6.1, 6.8, 7.1–7.3, 10.2, 10.4, 11.4, 15.4, 15.7, 15.8
- **Агент:** только UI: pure view state (texts)
- **Branch:** ralph/<date>-task-344

## Goal
Replace the mixed words of the UI with the glossary, so the same thing reads the same on every screen.

## Allowed to change
- `paratech/desktop/frontend/src/*View.ts`, `src/homeworkTurnkey.ts`, `src/popup/view.ts`, `src/sourcePicker/view.ts`,
  `src/nav.ts` (labels only), new `src/statusWords.ts`
- String literals only in `src/pages/{RecordingsPage,HomeworkPage,KnowledgeLessonPage,SummaryParts,KnowledgeBasePage}.tsx`
- their tests

## Out of scope
- Layout, buttons moved or removed (TASK-345…347), the rail (TASK-348), the system folder name «ДЗ».

## Acceptance criteria
- [ ] «Кратко», «Краткое содержание» → «Конспект»; «Мой конспект» stays.
- [ ] «Занятия»/«Начинается занятие» → «Пары»/«Начинается пара»; «Пара не найдена» stays.
- [ ] One dictionary `statusWords.ts` for the waiting states («Ждёт интернет», «Ждёт пополнения минут», «Ждёт зарядки»),
      used by activityView, journalView, recordingRowView, homeworkView.
- [ ] «Распознавание (речи)» → «Расшифровка»; «компонент распознавания речи» → «модуль расшифровки».
- [ ] «Записи и задачи»/«Записи и задания» → «Записи» (titles, popup and source-picker texts: «…остановить можно в окне
      paratech, раздел «Записи»»); «Домашние задания» → «Задания».
- [ ] «Предмет»/«Папка» buttons → «Добавить предмет»/«Новая папка»; «Предмет и тип» → «Исправить предмет или тип пары».
- [ ] «Сгенерировать ДЗ» → «Написать черновик»; «…подготовит сервис конспектов…» → «Черновика пока нет. paratech может
      написать его по тексту задания.»; «Адрес получателя домашки» → «Адрес получателя заданий»; «ДЗ» in journal texts → «задание».
- [ ] Turnkey: caption «paratech задаст 1–2 вопроса и напишет работу файлом (Word или PowerPoint). Разбор расходует
      минуты обработки.», button «Начать разбор»; the caption «Ответ» over an answer removed (turnkeyView, workView).
- [ ] All skips read «Пропустить вопросы»; «Понял так» → «План»; «Стоп» → «Остановить»; plan approve «Да» → «Да,
      выполнить»; «идёт просьба» → «идёт запрос».
- [ ] Tickets «Найти билеты» → «Разобрать список», «Застряло» → «Подбор остановлен»; journal «Оценено задание» →
      «Задание разобрано», «Запущен поиск ДЗ» → «Поиск заданий в записи пары».
- [ ] Existing tests updated; no text node built from model or imported text changes.
- [ ] UX check of coordinator-round.md 3a; `npm run test` and `npm run build` pass.

## Notes
Dropped from the audit as already fixed or against an owner decision: А-0.5 «Мои заметки» (owner's term is «Мой
конспект»), А-5.3 default tab (owner: «Мой конспект» first; ADR-0034 §2 sets the opening tab), А-12.4 and the
free-work half of А-15.4 (TASK-342), А-15.5 (TASK-337).
