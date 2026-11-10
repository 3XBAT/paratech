# Agent parity: what the student can do in the UI and what the agent can do

- **Date:** 2026-11-10 (catalog 10 built on the Hub, TASK-326 switches the desktop; owner 10.11 «агент может всё»,
  ADR-0038; tasks from TASK-431). Previous audit: 2026-10-28.
- **Rules:** [ADR-0033](adr/0033-agent-parity.md) (classes, waves, cards), [ADR-0038](adr/0038-agent-can-do-everything.md)
  (everything except the closed list of UI-only reasons, §2 there). Classes: ADR-0021 (`actionpolicy`).
- **Source of the rows:** every exported method of `paratech/desktop/internal/input/wails/*.go` on 2026-11-10 (a
  binding = a UI action), the pages in `paratech/desktop/frontend/src/pages/`, and pending tasks that add bindings
  («TASK-NNN, в работе»). Bindings added after 28.10 are marked *new*.

Legend
- **Class** — what the action is: **R** read; **U** reversible local change, done at once with «Отменить»; **C** the
  agent proposes a card, the student's click («Да» or the card button) runs it; **UI** a card or control only;
  **только UI (n)** — stays manual for reason *n* of ADR-0038 §2; *кнопка* — the agent can show the page's own button
  (`offer_button`).
- **«Да» / план** — **—** none; **план** = a plan card with one «Да» (a second write, a costly paid job, replacing a
  ready result); **«Да»** = a confirmation card; **клик** = the button itself is the consent.
- **Волна** — the catalog that brings the tool: **≤9** done, **10** TASK-326, **11**, **12**, **13** (waits for a
  feature task); **—** never (UI-only).
- Paid U jobs: one cheap job (≤ $0.05) without a plan; above it a plan (ADR-0021; ADR-0033 О-1 а).

## 1. Предметы и папки

| Действие в UI | Binding | Инструмент агента | Класс | «Да» / план | Волна | Notes |
|---|---|---|---|---|---|---|
| Список предметов | `LibraryService.Subjects` | `list_subjects` | R | — | ≤9 | catalog ≥ 11: `call_links` per kind instead of `call_link` |
| Обзор предмета | `SubjectOverview`, `MarkSubjectOpened` *new* | `list_subjects`, `find_lessons`, `list_assignments` | R | — | ≤9 | `MarkSubjectOpened` is view state |
| Создать предмет | `CreateSubject` | `create_subject` | U | 2-я запись → план | ≤9 | |
| Завести предметы из расписания | `ScheduleSubjects`, `CreateFromSchedule`, `UndoFromSchedule` *new* | `propose_subjects_from_schedule` | C (пакет) | один «Да» | 10 | |
| Подсказки «предмет из расписания» | `Suggestions` | via `add_alias` (TASK-329) | R | — | 11 | IDs and counters only |
| Добавить написание (алиас) | `AddAlias` | `add_alias` | U | — | 11 | TASK-329; a suggestion ID, never model text |
| Создать папку | `CreateFolder` | `create_folder` | U | 2-я запись → план | ≤9 | |
| Удалить пустую папку | `RemoveFolder` | `remove_folder` | U | — | 11 | TASK-329 |
| Удалить пустой предмет | `RemoveSubject` | `remove_subject` | U | — | 11 | TASK-329 |
| Удалить предмет с конспектами и файлами / «Вернуть» | `DiscardSubject`, `RestoreSubject` *new* | `remove_subject` (Undo = restore) | U | план всегда | 11 | TASK-329 |
| Лектор / семинарист / лабораторные | `SetSubjectTeacher`, `ClearSubjectTeacher` | `set_teacher`, `clear_teacher` | C | «Да» с точным адресом | 11 | TASK-327 specs, TASK-438 cards |
| **Ссылки на пару: общая и по типу пары** | `SetSubjectCallLink`, `ClearSubjectCallLink`, `CreateSubjectWithCallLink` *new*; по типу — TASK-431/432 | `set_call_links` (пакет строк) | C (пакет) | один «Да», точные ссылки | 11 | ADR-0038 §4: the model extracts rows from the student's text; skipped rows with reasons (очно, не подтверждена, нет предмета, не ссылка, две разные) |
| «Подключиться» | `OpenSubjectCallLink` *new* | `offer_button{open_call_link}` | UI, *кнопка* | клик | 11 | link of the lesson's kind, else general (TASK-436) |

## 2. Файлы (библиотека)

| Действие в UI | Binding | Инструмент агента | Класс | «Да» / план | Волна | Notes |
|---|---|---|---|---|---|---|
| Добавить файл через диалог | `ImportFile`, `ImportFileTo` *new* | — | только UI (4), *кнопка* | клик | — | a file dropped in the panel is an upload |
| Перетащить файл в окно / в панель | `librarydrop`, `AgentPanel.Attach` | `inspect_upload` | R (платно) | — | ≤9 | |
| Список файлов предмета / «Входящие» | `Materials` | `list_materials` | R | — | 11 | TASK-439; IDs, type, folder, counters; no names |
| Подсказка, куда положить | `PlacementHint` *new* | in `list_materials` rows (subject and folder IDs) | R | — | 11 | TASK-439 |
| Пары в папках предмета | `KnowledgeBaseService.FolderLessons` (TASK-429) | `find_lessons` | R | — | ≤9 | view only |
| Разложить из «Входящих» | `Place` | `place_upload` | U | 2-я запись → план | ≤9 | |
| Перенести разложенный файл | `Place` | `move_material` | U | — | 11 | TASK-439; Undo = previous place |
| Вернуть во «Входящие» | `MoveToInbox` | `move_to_inbox` | U | — | 11 | TASK-329 |
| Привязать к паре / отвязать | `Link`, `Unlink` | `link_material` / `unlink_material` | U | — | ≤9 / 11 | |
| Удалить файл / вернуть | `RemoveMaterial`, `RestoreMaterial` | `remove_material` | U | — | 11 | TASK-329 |
| Открыть файл в Word/PowerPoint | `OpenMaterial` | `offer_button{open_material}` | UI, *кнопка* | клик | 10 | |

## 3. «Мой конспект»

| Действие в UI | Binding | Инструмент агента | Класс | «Да» / план | Волна | Notes |
|---|---|---|---|---|---|---|
| Конспекты пары / предмета | `NotesService.ForLesson`, `ForSubject` | `list_notes` | R | — | 10 | catalog 12: part IDs for `note{lesson}` |
| Добавить файлы | `AddFiles` (диалог) | `note{add_upload}` | U; диалог — только UI (4) | — | 10 | |
| «Запиши в конспект: …» / вставить | `Paste` | `note{add_text}` | U | — | 10 | the student's words verbatim |
| Писать / править текст | TASK-291/292, в работе | `note{replace_text}` | U | — | 13 | TASK-453; Undo = previous revision |
| Заменить файл / отменить замену | `ReplaceFile`, `UndoReplace` | `note{replace_file}` | U | — | 12 | TASK-452 |
| Порядок частей | `Reorder` | `note{reorder}` | U | — | 12 | TASK-452 |
| Перенести, переименовать, удалить / вернуть | `Reanchor`, `Retitle`, `Remove`, `Restore` | `note{move, rename, remove}` | U | — | 10 | |
| Удалить навсегда | ADR-0027, не построено | — | C | «Да» | — | row when it is built |
| Распознать фото / «Принять» текст | TASK-167/169, в работе | — / только UI (5) | U / только UI (5) | — | — | «Принять» is the student's review |

## 4. Задания

| Действие в UI | Binding | Инструмент агента | Класс | «Да» / план | Волна | Notes |
|---|---|---|---|---|---|---|
| Список, открыть | `ListAssignments`, `OpenAssignment` | `list_assignments`, `reply.assignments` | R | — | ≤9 | |
| Добавить | `CreateAssignment` | `create_assignment` | U | 2-я → план | ≤9 | |
| Изменить, удалить, вернуть | `EditAssignment`, `DeleteAssignment`, `RestoreAssignment`, `DeleteImpact` *new* | `change_assignment` | U; с событием в Google — C | с событием — «Да» | 10 | `DeleteImpact` feeds the card |
| «Выполнено» / «Вернуть в работу» | `MarkDone`, `RestoreDone` *new* | `mark_assignments_done` | U | — | 10 | |
| Исправить предмет у извлечённого | `CorrectLesson` | `change_assignment{edit, subject}` | U | — | 10 | |
| Исправить преподавателя у извлечённого | `CorrectLesson` | `change_assignment{edit, teacher}` | U | — | 12 | TASK-452; a label, never an address |
| Срок в Google Календарь | `AgentConfirmService.ProposeDeadline` | `propose_deadline` | C | «Да» или разрешение | ≤9 | |
| «Обновить событие?» после правки срока | TASK-277/279, в работе | агент обновляет сам (C-4) | C | — | ≤9 | |
| «Сделать задание» (короткий ответ или под ключ) | `HomeworkService.Generate`; `DoAssignment` (TASK-411) | `do_assignment` | U (платно) | план для работы под ключ | 12 | TASK-452 after TASK-411, ADR-0038 О-5 |
| Правка черновика | `EditDraft` | — («Переписать» с указаниями) | только UI (5) | — | — | the agent rewrites through the job, not in the editor |
| Версии и статус | `DraftVersion`, `DraftStatus`, `GenerationStatus` | `job_status` | R | — | ≤9 | |
| Сохранить DOCX | `ExportDraft`, `ExportDocx`, `SaveDocx` | `offer_button{save_draft}` | только UI (4), *кнопка* | клик | 11 | TASK-440 |

## 5. Работа под ключ

| Действие в UI | Binding | Инструмент агента | Класс | «Да» / план | Волна | Notes |
|---|---|---|---|---|---|---|
| Разбор | `StartForAssignment` | `triage_assignment` | U (платно) | план, если дорого | ≤9 | |
| Ответить на вопросы / пропустить | `AnswerQuestions`, `SkipQuestions` | карточка `ask_questions` | UI | — | ≤9 | |
| «Как я понял задание»: поправить / подтвердить | `EditUnderstanding`, `ConfirmRequirements` | — | только UI (5) | клик | — | the student's review |
| Писать работу | `StartWriting` | `write_work` | U (платно) | план всегда | ≤9 | |
| План работы: одобрить / править | `ApproveOutline`, `EditOutline`, `EditOutlineAt` | — | только UI (5) | клик | — | |
| Остановить написание | `StopWriting` | `work{stop}` | U без «Отменить» | — | 12 | TASK-446; the line offers «Продолжить» |
| Переписать | `Regenerate` | `work{regenerate}` | U (платно) | план (заменяет готовое) | 12 | TASK-446 |
| Повторить шаг | `Retry` | `work{retry}` | U (платно) | — | 12 | TASK-446 |
| «Спросить по шагу» | `AskStep` | `work{ask}` | R (платно) | — | 12 | TASK-446; answer in a card |
| Работа и статус | `Work`, `WorkOfAssignment` | `job_status` | R | — | ≤9 | |
| Сохранить PDF | `ExportPDF` | `offer_button{save_as}` | только UI (4), *кнопка* | клик | 10 | |

## 6. Отправка преподавателю

| Действие в UI | Binding | Инструмент агента | Класс | «Да» / план | Волна | Notes |
|---|---|---|---|---|---|---|
| Подготовить отправку | `PrepareSend`, `ProposeSendWork` | `propose_send` | C | «Да» или разрешение | ≤9 | |
| «Да» / отказаться | `ConfirmSend`, `Confirm`, `CancelSend`, `Decline` | — | только UI (7) | клик | — | hard rule 3 |
| Статус | `Deliveries` | `delivery_status` | R | — | ≤9 | |
| «Отправлено вручную» | `MarkSent` | `delivery_outcome{mark_sent}` | C | «Да» | 13 | TASK-451 |
| «Отправить ещё раз» | `SendAgain` | `delivery_outcome{send_again}` | C | «Да» всегда | 13 | TASK-451; hard rule 7, never by permission |
| «Отправить в…» | TASK-239…241, в работе | `propose_send{at, time}` | C | «Да» | 13 | TASK-451 |
| Запланированные: время, отмена | TASK-242/243, в работе | `scheduled_send` | C | «Да» | 13 | TASK-451 |

## 7. Записи, расшифровки, конспекты paratech

| Действие в UI | Binding | Инструмент агента | Класс | «Да» / план | Волна | Notes |
|---|---|---|---|---|---|---|
| Начать / остановить запись | `PromptService.Start`, `StartRecording`, `ActivityService.StopRecording` | `recording{start, stop}` | U / U без «Отменить» | — | 10 | |
| Пропустить пару в окне записи | `PromptService.Skip` | `recording{skip_prompt}` | U | — | 12 | TASK-447 |
| Источники звука, сменить | `Sources`, `SelectSource`, `SwitchSource` | `list_sources`, `recording{switch_source}` | R / U | — | 12 | TASK-447; source IDs and app enums, no window titles |
| Окно записи: текущая пара, ссылка | `PromptService.Current`, `CallSubject` *new* | — | только UI (7) | — | — | popup state |
| Удалить записи | `RecordingDeleteService.Preview`, `Delete` | `propose_delete_recording` | C | «Да» | ≤9 | |
| Импорт старой записи | `RecordingImportService.Import` (диалог) | — | только UI (4), *кнопка* | клик | — | audio from the panel: ADR-0038 О-7 |
| Записи и задачи | `ListRecordings`, `ListJobs` | `find_lessons`, `job_status` | R | — | ≤9 | |
| Предмет и тип пары у записи | `SetLessonInfo` | `set_lesson_info` | U | — | ≤9 | |
| «Запустить сейчас» | `RunJobNow` | `job{run_now}` | U без «Отменить» | — | 12 | TASK-447 |
| «Отменить расшифровку» / «Расшифровать снова» | `CancelTranscription`, `Retranscribe` *new* | `job{cancel_transcription, retranscribe}` | U | — | 12 | TASK-447; cancel is undone by retranscribe |
| «Повторить», текст лекции, «Сделать конспект заново» | `RetryPostProcessing`, `RedoSummary` | `make_summary`, `find_homework`, `make_lecture_text` | U (платно) | лекция / замена — план | ≤9 | |
| Звук, совет, «Скрыть» | `RecordingSound`, `Advice`, `DismissAdvice` | — | только UI (7) | — | — | status and hints |
| Открыть конспект / текст лекции / расшифровку | `SummaryService.View`, `TranscriptService.*` | `offer_button{open_*}` | UI, *кнопка* | клик | 10 | content questions → `answer_question` |
| Копировать | `SummaryService.CopyAll`, `TranscriptService.CopyAll/CopyText`, `LectureTextService.Copy` | `offer_button{copy_*}` | UI, *кнопка* | клик | 11 | TASK-440 |
| Сохранить как | `SummaryService.SaveAs`, `TranscriptService.SaveAs`, `LectureTextService.SaveAs`, `ExportSubject` | `offer_button{save_*, export_subject}` | только UI (4), *кнопка* | клик | 11 | TASK-440 |

## 8. База знаний и билеты

| Действие в UI | Binding | Инструмент агента | Класс | «Да» / план | Волна | Notes |
|---|---|---|---|---|---|---|
| Поиск, вопрос по лекциям | `Search`, `AnswerService.Answer` | `search`, `answer_question` | R / R (платно) | — | ≤9 | |
| Предметы и лекции базы | `Subjects`, `Lessons`, `Lectures`, `Lesson`, `LessonByKey` | `find_lessons` | R | — | ≤9 | |
| Билеты из вставленного списка | `TicketService.Propose` | `tickets{propose_text}` | U | — | 12 | TASK-445; the student's pasted list verbatim, the page's parser (ADR-0038 §5) |
| Билеты из файла | `ProposeFromMaterial` | `tickets{propose_material}` | U | — | 12 | TASK-445 |
| Подтвердить (платно) | `Confirm` | `tickets{confirm}` | U (платно) | план | 12 | TASK-445 |
| Отменить / вернуть | `Cancel`, `Undo` | `tickets{cancel, restore}` | U | — | 12 | TASK-445 |
| Списки билетов | `List`, `Get` | `list_tickets` | R | — | 12 | TASK-445; IDs, state, counts |

## 9. Расписание, Google Календарь

| Действие в UI | Binding | Инструмент агента | Класс | «Да» / план | Волна | Notes |
|---|---|---|---|---|---|---|
| Импорт расписания (диалог) | `ScheduleImportService.ChooseFile` | — | только UI (4), *кнопка* | клик | — | |
| ICS, брошенный в панель → превью → применить | `Commit`, `Discard` | `propose_schedule_import{upload}` | C | «Да» на превью | 12 | TASK-450; the same preview as the page |
| Пары на сегодня / неделю | `TodayService.Today`, `SubjectPosts` | `find_lessons`, `list_assignments` | R | — | ≤9 | |
| Синхронизировать с Google | `GoogleCalendarSync` | `offer_button{google_sync}` | C, *кнопка* | клик | 10 | |
| Статус календаря | `GoogleCalendarStatus` | `get_settings` (state only) | R | — | 11 | TASK-440 |

## 10. Чаты, журнал, настройки, подключения

| Действие в UI | Binding | Инструмент агента | Класс | «Да» / план | Волна | Notes |
|---|---|---|---|---|---|---|
| Новый чат, форк, «Начать заново отсюда» | `NewChat`, `ForkChat`, `Ask(restartFrom)` | — | только UI (7) | — | — | |
| Название нового чата | — | `reply.chat_title` | U | — | 11 | TASK-330 |
| Переименовать чат | `RenameChat` | `chat{rename}` | U | — | 12 | TASK-448; only on the student's explicit request (ADR-0038 О-4) |
| Архивировать / вернуть чат | `ArchiveChat`, `RestoreChat` | `chat{archive, restore}` | U | — | 12 | TASK-448; never the current chat |
| Список чатов, ранние сообщения | `Chats`, `ChatEarlier`, `ChatCurrent`, `MentionCandidates` | `list_chats` | R | — | 12 | TASK-448; IDs, dates, counters, no titles |
| Журнал действий | `AgentService.History` | `list_actions` | R | — | 12 | TASK-448 |
| «Отменить» действие / всё | `Undo`, `UndoRequest` | `undo{action}`; `offer_button{undo_action}` | U без «Отменить» | — | 12 | TASK-448; the same compensation |
| Управление агентом | `ApprovePlan`, `RevisePlan`, `Answer`, `Stop`, `Continue`, `Retry`, `Restart`, `Rerun`, `Cancel`, `Withdraw` *new*, `AgentConfirmService.*` | — | только UI (7) | клик | — | |
| Окно записи, только от сети, нагрузка | `SetRecordingPromptsEnabled`, `SetProcessOnlyOnACPower`, `SetRecognitionLoad` | `set_setting` | U | — | 10 | |
| События при смене срока | TASK-276 (done) / 279 | `set_setting{calendar_update_on_due_change}` | U | — | 11 | TASK-331 |
| «ИИ распознаёт фото конспектов» | `SetNoteRecognition` *new* | `set_setting{notes_ocr}` | U (`off`/`local`) | — | 11 | TASK-331; a future `cloud` is C |
| Настройки (чтение) | `GetSettings` | `get_settings` | R | — | 10 | catalog 11: + Google, mail, Hub key states (TASK-440) |
| Разрешения | `Permissions`, `SetPermission` (TASK-360) | — | только UI (6) | — | — | ADR-0034 §5 |
| Профиль | `GetProfile`, `SaveProfile` | `set_profile_field`; чтение — `get_profile` | C / R | «Да» с точным значением | 11 / 12 | TASK-438; reading: TASK-449, ADR-0038 О-6 |
| Почта: адрес получателя | `SaveMailSettings` (recipient) | `set_mail_recipient` | C | «Да» с точным адресом | 12 | TASK-449 |
| Почта: сервер, порт, защита, логин | `SaveMailSettings` | `offer_button{open_settings}` | только UI (2), *кнопка* | — | — | the password is sent there |
| Пароль почты, ключ Hub, Google Client ID/secret | `SetMailPassword`, `RemoveMailPassword`, `SetHubKey`, `RemoveHubKey`, `SetGoogleClient`, `ClearGoogleClient` | `offer_button{open_settings}` | только UI (1), *кнопка* | — | 11 (кнопка) | TASK-440 |
| Адрес Hub | `SaveHubURL` | `offer_button{open_settings}` | только UI (2), *кнопка* | — | 11 (кнопка) | |
| Проверить почту | `CheckMail` | `offer_button{check_mail}` | C, *кнопка* | клик | 11 | TASK-440 |
| Квота | `GetConnections`, `RefreshQuota` | `quota_status` | R | — | 10 | |
| Войти в Google | `GoogleSignIn`, `GoogleCancelSignIn`, `GoogleStatus` | `offer_button{google_sign_in}` | только UI (3), *кнопка* | клик | 10 | |
| Выйти из Google | `GoogleSignOut` | `offer_button{google_sign_out}` | C, *кнопка* | клик | 11 | TASK-440 |
| Модели: список, по умолчанию | `ModelsService.List`, `SetDefault` | `list_models`, `model{set_default}` | R / U | — | 12 | TASK-449 |
| Модели: скачать, отменить | `PreflightDownload`, `StartDownload`, `CancelDownload`, `DiscardPartial`, `OpenSourcePage`, `OpenModelsHomePage` | `offer_button{download_model}` | C, *кнопка* | клик | 10 | network, gigabytes |
| Удалить модель | `ModelsService.Delete` | `model{delete}` | C | «Да» | 12 | TASK-449 |
| Модели: импорт файла | `ChooseImportFile`, `ConfirmImport`, `DiscardImport`, `ImportCatalogFile` | — | только UI (4) | — | — | |
| «Пропуск тишины» | `SpeechService.Status`, `StartInstall`, `CancelInstall` | `offer_button{install_speech}` | C, *кнопка* | клик | 11 | TASK-440 |
| Проверка звука | `StartSoundCheck`, `SoundCheckStatus`, `StopSoundCheck` | `offer_button{sound_check}` | только UI (8), *кнопка* | клик | 10 | |
| Пакет для поддержки | `SaveSupportBundle` | `offer_button{save_support_bundle}` | только UI (4), *кнопка* | клик | 11 | TASK-440 |
| Справка | `HelpService.OpenHelpPage` | `offer_button{open_help}` | UI, *кнопка* | клик | 10 | |
| Hub на этом компьютере (бета) | `LocalHubService.*` (TASK-389…391) | — | только UI (1) | — | — | beta tooling (ADR-0036) |
| «Проверить снова» (сервер не отвечает) | `ActivityService.RecheckNetwork` (TASK-424) | — | только UI (7) | — | — | ADR-0037 |

Not actions (no row): `Shell.*`, `PopupWindowService.Fit`, `AgentPanel.SetFills/Attach/Publish/Draft`, the media
handler, pickers, `WailsLogger`.

## 11. Tasks

- Wave 10: TASK-326 (desktop switch; the owner's 10.11 instruction is the choice to switch it on, ADR-0038 D-1).
- Call links per kind (usable from the UI before the agent): TASK-431 (domain, migration, use case), TASK-432
  (bindings, «Подключиться» by kind), TASK-433 (UI), TASK-435 (batch service).
- Wave 11: TASK-327 (specs, as written minus `set_call_link`/`clear_call_link`), TASK-434 (ADR-0038 specs),
  TASK-436 (`set_call_links`), TASK-437 (its card), TASK-438 (teacher and profile cards; replaces TASK-328), TASK-329
  (library), TASK-330 (chat title), TASK-331 (two settings), TASK-439 (materials), TASK-440 (buttons, settings
  states), TASK-441 (cards UI; replaces TASK-332), TASK-333 + TASK-335 (plan runner), TASK-442 (Hub panel v11;
  replaces TASK-334), TASK-443 (switch to 11; replaces TASK-336).
- Wave 12: TASK-444 (specs), TASK-445 (tickets), TASK-446 (work controls), TASK-447 (recordings and jobs), TASK-448
  (chats, journal, undo), TASK-449 (models, recipient, reading values), TASK-450 (ICS from the panel), TASK-452 (note
  parts, assignment teacher, «Сделать задание»), TASK-454 (cards UI), TASK-455 (Hub panel v12), TASK-456 (switch to 12).
- Wave 13 (waits for features): TASK-451 (sending: time, reschedule, outcome cards; after TASK-239/242), TASK-453
  (note text edit; after TASK-291). Their Hub and switch tasks are filed when they are ready.
