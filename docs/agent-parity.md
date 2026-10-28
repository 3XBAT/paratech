# Agent parity: what the student can do in the UI and what the agent can do

- **Date:** 2026-10-28 (catalog 8 on master; TASK-308 is the latest task)
- **Rule and mechanics:** [ADR-0033](adr/0033-agent-parity.md). Classes: ADR-0021 (`actionpolicy`).
- **Source of the rows:** every exported method of the services in `paratech/desktop/internal/input/wails/*.go`
  (a binding = a UI action), the pages in `paratech/desktop/frontend/src/pages/`, and the pending tasks that add
  bindings (marked «TASK-NNN, в работе»).

Legend
- **Class** — what the action is, not what exists today: **R** read; **U** reversible local change, the agent
  does it at once with «Отменить»; **C** the agent only proposes, the student's click («Да» or the card button) runs it
  (irreversible, external, or a batch the desktop builds); **UI** card or control only; **только UI** — stays a manual
  action (reason in the notes); with *кнопка* the agent can still show the page's own button on a card
  (`offer_button`, ADR-0033), and the student's click is the action.
- **«Да» / план** — **—** none; **план** = a plan card with one «Да» (a second write, a costly paid job, replacing a
  ready result, `AlwaysPlan`); **«Да»** = a confirmation card per effect; **клик** = the button itself is the consent.
- Paid U jobs: as today, one cheap job (≤ $0.05) without a plan; above it a plan (ADR-0021; see ADR-0033 Open О-1).

## 1. Предметы и папки

| Действие в UI | Binding | Инструмент агента сейчас | Класс | «Да» / план | Notes |
|---|---|---|---|---|---|
| Список предметов | `LibraryService.Subjects` | `list_subjects` | R | — | |
| Обзор предмета | `LibraryService.SubjectOverview`, `MarkSubjectOpened` | частично: `list_subjects`, `find_lessons`, `list_assignments` | R | — | `MarkSubjectOpened` is view state, not an action |
| Создать предмет | `LibraryService.CreateSubject` | `create_subject` (без написаний) | U | 2-я запись → план | |
| **Завести предметы из расписания** | нет (по одному: `Suggestions` + `CreateSubject` / `AddAlias`) | нет | C (пакет) | один «Да» | gap P1; the desktop builds the list, see §11 |
| Подсказки «предмет из расписания» | `LibraryService.Suggestions` | нет | R | — | result to the planner: IDs and counters only, never the strings (ADR-0022) |
| Добавить написание (алиас) | `LibraryService.AddAlias` | нет | U | 2-я запись → план | the alias is a suggestion ID, never model text |
| Создать папку | `LibraryService.CreateFolder` | `create_folder` | U | 2-я запись → план | |
| Удалить пустую папку | `LibraryService.RemoveFolder` | нет | U | — | Undo re-creates the folder with the same ID |
| Удалить пустой предмет | `LibraryService.RemoveSubject` | нет (только «Отменить» у `create_subject`) | U | — | |
| Удалить предмет с конспектами и файлами, «Вернуть» 3 дня | TASK-288/289, в работе | нет | U | план всегда | hides N notes and M files; Undo = restore within the window |
| Лектор / семинарист / лабораторные: задать, очистить | `LibraryService.SetSubjectTeacher`, `ClearSubjectTeacher` | нет (ADR-0030: намеренно) | C | «Да» | an e-mail address is a recipient; ADR-0033 Open О-4 |
| Ссылка на звонок у предмета: вписать, убрать | TASK-259/261/264, в работе | нет (ADR-0032: намеренно) | C | «Да» | a URL; Open О-4 |

## 2. Файлы (библиотека)

| Действие в UI | Binding | Инструмент агента сейчас | Класс | «Да» / план | Notes |
|---|---|---|---|---|---|
| Добавить файл через диалог | `LibraryService.ImportFile` | — | только UI, *кнопка* | клик | native open dialog; in the panel a dropped file is an upload |
| Перетащить файл в окно / в панель | `librarydrop`, `AgentPanel.Attach` | `inspect_upload` | R (платно) | — | |
| Список файлов предмета / «Входящие» | `LibraryService.Materials` | нет | R | — | IDs, type, counters; no file names (ADR-0022) |
| Разложить в папку предмета | `LibraryService.Place` | `place_upload` (только из «Входящих») | U | 2-я запись → план | moving a filed material between folders: no tool |
| Вернуть во «Входящие» | `LibraryService.MoveToInbox` | нет (только «Отменить» у `place_upload`) | U | — | |
| Привязать к паре / отвязать | `LibraryService.Link`, `Unlink` | `link_material` / нет | U | — | |
| Удалить файл / вернуть | `LibraryService.RemoveMaterial`, `RestoreMaterial` | нет | U | — | soft delete, the blob is kept |
| Открыть файл в Word/PowerPoint | `LibraryService.OpenMaterial` | нет | UI, *кнопка* | клик | starts an external program |

## 3. «Мой конспект»

| Действие в UI | Binding | Инструмент агента сейчас | Класс | «Да» / план | Notes |
|---|---|---|---|---|---|
| Конспекты пары / предмета | `NotesService.ForLesson`, `ForSubject` | нет | R | — | IDs, anchor date and kind, part counters; no titles |
| Добавить файлы к паре | `NotesService.AddFiles` (диалог) | нет | U для файла из панели; диалог — только UI | — | gap P3: an upload dropped in the panel becomes a part |
| Вставить текст | `NotesService.Paste` | нет | U | — | whose words go in: ADR-0033 Open О-5 |
| Написать / править текст, «Вернуть предыдущую версию» | TASK-291/292, в работе | нет | только UI | — | editor with autosave; the model never writes note text |
| Заменить файл / отменить замену | `NotesService.ReplaceFile`, `UndoReplace` | нет | U | — | from an upload |
| Порядок частей | `NotesService.Reorder` | нет | только UI | — | drag and drop; low value for the agent |
| Перенести к другой паре / дню | `NotesService.Reanchor` | нет | U | — | |
| Переименовать | `NotesService.Retitle` | нет | U | — | the label is the student's words (as `create_assignment.title`) |
| Удалить / вернуть конспект | `NotesService.Remove`, `Restore` | нет | U | — | soft |
| Удалить навсегда | ADR-0027, не построено | нет | C | «Да» | purges blobs |
| Распознать фото (Windows OCR) / «Принять» текст | TASK-167/169, в работе | нет | U / только UI | — | «Принять» stays manual: the student reviews the text |

## 4. Задания

| Действие в UI | Binding | Инструмент агента сейчас | Класс | «Да» / план | Notes |
|---|---|---|---|---|---|
| Список заданий | `HomeworkService.ListAssignments` | `list_assignments` | R | — | |
| Открыть задание | `HomeworkService.OpenAssignment` | частично: `reply.assignments` (карточка) | R | — | |
| Добавить задание | `HomeworkService.CreateAssignment` | `create_assignment` | U | 2-я → план | |
| Изменить название, заметку, срок, предмет | `HomeworkService.EditAssignment` | `edit_due` (только срок) | U | — | gap P2 |
| Удалить / «Вернуть» (3 дня) | `HomeworkService.DeleteAssignment`, `RestoreAssignment` | нет | U | — | gap P2; with a Google event see Open О-6; the agent can restore only what it deleted in this chat (no list of deleted ones, ADR-0029 C-2) |
| Исправить предмет / преподавателя у извлечённого | `HomeworkService.CorrectLesson` | нет | U | — | subject: through `edit_assignment`; the teacher text stays manual (ADR-0030) |
| Предложить срок в Google Календарь | `AgentConfirmService.ProposeDeadline` | `propose_deadline` | C | «Да» | |
| «Обновить событие в Google Календаре?» после правки срока | TASK-277/279, в работе | нет (агентский `edit_due` обновляет сам, C-4) | C | «Да» | Open О-2 |
| Быстрый черновик | `HomeworkService.Generate` | нет (убран из агента, владелец 2026-10-26) | U (платно) | — | stays UI by the owner's decision |
| Правка черновика | `HomeworkService.EditDraft` | — | только UI | — | the student's text in the editor |
| Версии и статус черновика | `DraftVersion`, `DraftStatus`, `GenerationStatus` | `job_status` | R | — | |
| Сохранить DOCX / экспорт | `ExportDraft`, `ExportDocx`, `SaveDocx` | нет | только UI, *кнопка* | клик | «Сохранить как» only (ADR-0022) |

## 5. Работа под ключ

| Действие в UI | Binding | Инструмент агента сейчас | Класс | «Да» / план | Notes |
|---|---|---|---|---|---|
| «Сделать под ключ» (разбор) | `TurnkeyService.StartForAssignment` | `triage_assignment` | U (платно) | план, если дорого или «частично» | |
| Ответить на вопросы / «Пропустить» | `AnswerQuestions`, `SkipQuestions` | карточка `ask_questions` | UI | — | |
| «Как я понял задание»: поправить / «Подтвердить» | `EditUnderstanding`, `ConfirmRequirements` | в плане `write_work` | только UI / UI | клик | the student's edit |
| Писать работу | `TurnkeyService.StartWriting` | `write_work` | U (платно) | план всегда | |
| План работы: одобрить / править | `ApproveOutline`, `EditOutline`, `EditOutlineAt` | нет | только UI | клик | the approval is the student's |
| Остановить написание | `TurnkeyService.StopWriting` | только «Отменить» у `write_work` | C, *кнопка* | клик | |
| Переписать | `TurnkeyService.Regenerate` | нет | U (платно) | план (заменяет готовое) | |
| «Спросить по шагу» (помощник) | `TurnkeyService.AskStep` | нет | R (платно) | — | |
| Повторить шаг | `TurnkeyService.Retry` | нет | U (платно) | — | |
| Работа и её статус | `Work`, `WorkOfAssignment` | `job_status` | R | — | |
| Сохранить PDF | `TurnkeyService.ExportPDF` | нет | только UI, *кнопка* | клик | save dialog |

## 6. Отправка преподавателю

| Действие в UI | Binding | Инструмент агента сейчас | Класс | «Да» / план | Notes |
|---|---|---|---|---|---|
| Подготовить отправку (черновик / работа) | `HomeworkService.PrepareSend`, `AgentConfirmService.ProposeSendWork` | `propose_send` | C | «Да» | |
| «Да» в карточке | `HomeworkService.ConfirmSend`, `AgentConfirmService.Confirm` | — | клик студента | «Да» | by design (hard rule 3) |
| Отказаться | `HomeworkService.CancelSend`, `AgentConfirmService.Decline` | — | UI | клик | |
| Статус отправки | `HomeworkService.Deliveries` | `delivery_status` | R | — | |
| «Отправлено вручную» (неизвестный исход) | `HomeworkService.MarkSent` | нет | C, *кнопка* | клик | the student's statement about the outside world |
| «Отправить ещё раз» (неизвестный исход) | `HomeworkService.SendAgain` | нет | C, *кнопка* | клик | hard rule 7: never a blind retry |
| «Отправить в…» (время) | TASK-239…241, в работе | нет | C | «Да» | the agent may prefill the time from the request; the card confirms it |
| Запланированные: сменить время, отменить, решить опоздавшую | TASK-242/243, в работе | нет | C | «Да» | |

## 7. Записи, расшифровки, конспекты paratech

| Действие в UI | Binding | Инструмент агента сейчас | Класс | «Да» / план | Notes |
|---|---|---|---|---|---|
| Начать запись (окно перед парой / вручную) | `PromptService.Start`, `StartRecording` | нет | Open О-7 | Open О-7 | system audio capture |
| Пропустить пару в окне записи | `PromptService.Skip` | — | только UI | — | popup control |
| Сменить источник звука | `PromptService.SelectSource`, `ActivityService.SwitchSource` | нет | U | — | |
| Остановить запись | `ActivityService.StopRecording` | нет | Open О-7 | Open О-7 | the rest of the lesson is lost if wrong |
| Удалить записи | `RecordingDeleteService.Preview`, `Delete` | `propose_delete_recording` | C | «Да» | |
| Импорт старой записи | `RecordingImportService.Import` (диалог) | нет | только UI, *кнопка*; аудио из панели — U позже | — | audio stays local (hard rule 2) |
| Записи и задачи | `ActivityService.ListRecordings`, `ListJobs` | `find_lessons`, `job_status` | R | — | |
| Предмет и тип пары у записи | `KnowledgeBaseService.SetLessonInfo` | `set_lesson_info` | U | — | |
| «Запустить сейчас» (от батареи) | `ActivityService.RunJobNow` | нет | C, *кнопка* | клик | no compensation |
| «Повторить» обработку / текст лекции | `ActivityService.RetryPostProcessing` | `make_summary`, `find_homework`, `make_lecture_text` | U (платно) | лекция — план всегда | |
| «Сделать конспект заново» | `ActivityService.RedoSummary` | `make_summary` | U (платно) | план (заменяет готовое) | |
| Звук записи, совет по источнику, «Скрыть» | `RecordingSound`, `PromptService.Advice`, `DismissAdvice` | — | только UI | — | status and hints |
| Посмотреть конспект / текст лекции / расшифровку | `SummaryService.View`, `TranscriptService.Status/Segments/Text` | нет (планировщик не видит тексты) | R, *кнопка* «Открыть» | — | content questions go to `answer_question` |
| Копировать | `SummaryService.CopyAll`, `TranscriptService.CopyAll/CopyText`, `LectureTextService.Copy` | нет | только UI, *кнопка* | клик | clipboard |
| Сохранить как | `SummaryService.SaveAs`, `TranscriptService.SaveAs`, `LectureTextService.SaveAs`, `KnowledgeBaseService.ExportSubject` | нет | только UI, *кнопка* | клик | save dialog (ADR-0022) |

## 8. База знаний и билеты

| Действие в UI | Binding | Инструмент агента сейчас | Класс | «Да» / план | Notes |
|---|---|---|---|---|---|
| Поиск | `KnowledgeBaseService.Search` | `search` | R | — | |
| Вопрос по лекциям | `AnswerService.Answer` | `answer_question` | R (платно) | — | Open О-1 |
| Предметы и лекции базы | `KnowledgeBaseService.Subjects/Lessons/Lectures/Lesson` | частично `find_lessons` | R | — | |
| Билеты из вставленного списка | `TicketService.Propose` | нет | U | — | the pasted list is foreign text: only from an upload, never through the planner |
| Билеты из файла | `TicketService.ProposeFromMaterial` | нет | U | — | |
| Подтвердить билеты (платно) | `TicketService.Confirm` | нет | U (платно) | план | |
| Отменить / вернуть | `TicketService.Cancel`, `Undo` | нет | U | — | |
| Списки билетов | `TicketService.List`, `Get` | нет | R | — | |

## 9. Расписание, ссылки на пару, Google Календарь

| Действие в UI | Binding | Инструмент агента сейчас | Класс | «Да» / план | Notes |
|---|---|---|---|---|---|
| Импорт расписания (диалог) | `ScheduleImportService.ChooseFile` | нет | только UI, *кнопка*; ICS из панели — превью | — | |
| Применить импорт | `ScheduleImportService.Commit` | нет | C | «Да» на превью | changes lessons and the Google projection |
| Отменить импорт | `ScheduleImportService.Discard` | — | UI | клик | |
| Пары на сегодня / неделю | `TodayService.Today`, `SubjectPosts` | `find_lessons`, `list_assignments` | R | — | |
| Ссылка на пару: «Открыть», «Копировать» | TASK-261, в работе | нет | UI, *кнопка* | клик | Go resolves the link by lesson ID; the planner never sees it |
| Ссылка на пару: вписать, убрать | TASK-261/264, в работе | нет | C | «Да» | Open О-4 |
| Синхронизировать с Google | `GoogleCalendarService.GoogleCalendarSync` | нет | C, *кнопка* | клик | network; mirrors confirmed state |
| Статус календаря | `GoogleCalendarService.GoogleCalendarStatus` | нет | R | — | |

## 10. Чаты, журнал, настройки, подключения

| Действие в UI | Binding | Инструмент агента сейчас | Класс | «Да» / план | Notes |
|---|---|---|---|---|---|
| Новый чат | `AgentService.NewChat` | — | только UI | — | the request already lives in a chat |
| Переименовать чат | `AgentService.RenameChat` | нет | U | — | Open О-8 (ADR-0028: a title is never model text) |
| Архивировать / вернуть чат | `AgentService.ArchiveChat`, `RestoreChat` | нет | U | — | only other chats: the current one has a running request |
| Форк, «Начать заново отсюда» | TASK-248/271, в работе | — | только UI | — | picks a message |
| Список чатов, ранние сообщения | `Chats`, `ChatEarlier`, `ChatCurrent`, `MentionCandidates` | — | только UI | — | |
| Журнал действий агента | `AgentService.History` | нет | R | — | |
| «Отменить» действие / всё | `AgentService.Undo`, `UndoRequest` | нет | UI, *кнопка* | клик | |
| «Да», «Стоп», «Продолжить», «Повторить», «Начать заново», «Отмена», ответы | `ApprovePlan`, `RevisePlan`, `Answer`, `Stop`, `Continue`, `Retry`, `Restart`, `Rerun`, `Cancel` | — | только UI | клик | control of the agent itself |
| Карточка бесплатной работы | `FreeWorkCard`, `FreeWork` | — | UI | клик | |
| Окно записи перед парой: вкл / выкл | `SettingsService.SetRecordingPromptsEnabled` | нет | U | — | gap P6 `set_setting` |
| Обрабатывать только от сети | `SettingsService.SetProcessOnlyOnACPower` | нет | U | — | |
| Нагрузка распознавания | `SettingsService.SetRecognitionLoad` | нет | U | — | |
| Предупреждать о работе на свободную тему | `SettingsService.SetFreeWorkNotice` | нет | U | — | |
| События в календаре при смене срока | TASK-276/279, в работе | нет | U | — | |
| «ИИ распознаёт фото конспектов» | TASK-294, в работе | нет | U (`off`/`local`); будущий `cloud` — C | «Да» для `cloud` | images would leave the PC |
| Настройки (чтение) | `SettingsService.GetSettings` | нет | R | — | |
| Профиль (ФИО, группа, вуз, факультет, город) | `SettingsService.GetProfile`, `SaveProfile` | нет | U | — | personal data through the planner: Open О-4 |
| Почта: адрес получателя, SMTP | `ConnectionsService.SaveMailSettings` | — | только UI | — | addresses and servers (ADR-0022) |
| Пароль почты | `SetMailPassword`, `RemoveMailPassword` | — | только UI | — | secret (hard rule 4) |
| Проверить почту | `ConnectionsService.CheckMail` | нет | C, *кнопка* | клик | network |
| Адрес Hub | `ConnectionsService.SaveHubURL` | — | только UI | — | trust boundary |
| Ключ Hub | `SetHubKey`, `RemoveHubKey` | — | только UI | — | secret |
| Квота | `GetConnections`, `RefreshQuota` | нет | R | — | «сколько осталось» |
| Google: Client ID / secret | `GoogleService.SetGoogleClient`, `ClearGoogleClient` | — | только UI | — | secret |
| Войти в Google | `GoogleService.GoogleSignIn`, `GoogleCancelSignIn` | — | только UI, *кнопка* | клик | OAuth consent in the browser |
| Выйти из Google | `GoogleService.GoogleSignOut` | нет | C, *кнопка* | клик | |
| Модели: скачать, отменить, страница источника | `ModelsService.PreflightDownload`, `StartDownload`, `CancelDownload`, `DiscardPartial`, `OpenSourcePage`, `OpenModelsHomePage` | нет | C, *кнопка* | клик | network, gigabytes |
| Модели: импорт файла | `ChooseImportFile`, `ConfirmImport`, `DiscardImport`, `ImportCatalogFile` | — | только UI | — | file dialog |
| Модель по умолчанию | `ModelsService.SetDefault` | нет | U | — | |
| Удалить модель | `ModelsService.Delete` | нет | C, *кнопка* | клик | |
| Компонент «пропуск тишины» | `SpeechService.Status`, `StartInstall`, `CancelInstall` | нет | C, *кнопка* | клик | download |
| Проверка звука | `DiagnosticsService.StartSoundCheck`, `SoundCheckStatus`, `StopSoundCheck` | нет | только UI, *кнопка* | клик | the student has to listen |
| Пакет для поддержки | `DiagnosticsService.SaveSupportBundle` | — | только UI | — | save dialog |
| Справка | `HelpService.OpenHelpPage` | нет | UI, *кнопка* | клик | |

Not actions (no row): `Shell.*`, `PopupWindowService.Fit`, `AgentPanel.Publish/Draft`, the media handler, pickers.

## 11. Gaps in priority order

Each gap becomes tasks only after the owner has seen this table (tasks.json is not changed in this round).

1. **Предметы из расписания** (owner 2026-10-28).
   - A pure domain service `library/services/schedulesubjects` groups the free schedule strings
     (`subjectmatch.Suggestion` plus the lesson kinds of each string): an **obvious** group (same name once the kind mark
     is cut — «(лек.)», «(сем.)», «(пр.)», «(лаб.)», «лекция», «семинар»…, case, «ё», spaces) is merged; a
     **doubtful** one (abbreviation, typo, a string close to an existing subject) is a separate row. The name is
     the cleaned schedule string, the raw strings become aliases, the lesson kinds are listed per group.
   - `cases/library`: `ScheduleSubjects(ctx)` (the proposal with a token over its content, as the schedule import) and
     `CreateFromSchedule(ctx, token, choices)`; one subject per transaction, the subject ID derived from the group key,
     so a retry creates nothing twice (ARCH-23, hard rule 7). Undo of the batch removes the subjects that are still
     empty.
   - UI: button «Завести предметы из расписания» on «Предметы» opens the review; the agent tool
     `propose_subjects_from_schedule` (class C, no arguments) shows the same review as a card: obvious groups checked,
     doubtful ones below with «Объединить / Отдельно / Не заводить» (default: Open О-12). One «Да» applies the
     checked rows. Nothing of the strings goes to the planner; the names reach it only after «Да», as subjects the
     student confirmed (ADR-0022).
   - Folders: every subject already gets the system folders «Лекции», «Семинары», «Лабораторные» (with «Материалы»,
     «ДЗ», «Билеты»; `subject/folder.go`) at creation, so nothing new is created; Open О-3 asks whether empty ones should be hidden.
2. **Задания: изменить, удалить, вернуть.** `edit_assignment` (title, note, due, time, subject; old values in the
   journal), `delete_assignment` (Undo = «Вернуть» within 3 days), restore through that Undo. Google event: Open О-6.
3. **«Мой конспект».** `list_notes` (R), `add_note_part` (an upload from the panel to a lesson / day / recording),
   `move_note`, `rename_note`, `remove_note` (U with Undo). Paste: Open О-5. Editing stays manual.
4. **Преподаватели** — after Open О-4.
5. **Ссылки на пару** — «Открыть» / «Копировать» through `offer_button`; typing a link after Open О-4.
6. **Настройки.** `get_settings` (R) and `set_setting` (U, a closed enum of setting and value; the old value is the
   compensation); `quota_status` (R). Profile after Open О-4.
7. **`offer_button`** (UI): a card with the page's own button for the rows marked *кнопка* (open, copy, save as,
   sign in to Google, sync, download a model, sound check, help, «Отменить»).
8. Библиотека: `move_to_inbox`, `unlink_material`, `remove_material` / restore, `add_alias`, `remove_folder`, removing a
   subject (after TASK-288).
9. Отправка: time in `propose_send` (after TASK-239), rescheduling and cancelling (after TASK-242), «Отправлено вручную» /
   «Отправить ещё раз» as buttons.
10. Билеты from a file, «Переписать» a work, «Остановить написание».
11. Записи: start / stop (after Open О-7), source switch, default model.
12. Чаты: rename / archive (after Open О-8), the action journal as R.
