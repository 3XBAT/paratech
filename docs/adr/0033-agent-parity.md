# ADR-0033: Agent parity — everything the student does in the UI, the agent can do

- **Status:** Accepted (owner answers 2026-10-28); Open О-10, О-11, О-13, О-14 remain
- **Date:** 2026-10-28
- **Source:** owner 2026-10-28 («вся работа с приложением может идти полностью через агента»); the audit
  `docs/agent-parity.md`; ADR-0021, ADR-0022, ADR-0023, ADR-0026…0030, ADR-0032; `domain/agent/values/tool.go`
  (catalog 8, 27 tools), `domain/agent/services/actionpolicy/policy.go`, `domain/agent/thread/budget.go`.
- **Amends:** ADR-0021 (class C wording, plan budget, one U tool without compensation), ADR-0022 (owner override
  О-4: addresses, links and profile values as tool arguments), ADR-0023 (tool layout of a step), ADR-0028 (title of a
  new chat, О-8), ADR-0029 (done assignments, §9), ADR-0030 and ADR-0032 (the agent may set a teacher or a call link
  through a «Да» card, О-4).

## Context
About 200 exported binding methods in `input/wails` are UI actions or reads; the agent has 23 tools (8 R, 12 U, 3
C) plus 4 card tools. Most gaps are local
reversible changes (edit or delete an assignment, notes, settings, library moves); some are external or
irreversible; some are UI-only by nature (secrets, OAuth consent, native file dialogs, the agent's own controls).
Three limits block a full agent: a request has ≤ 6 writes, ≤ 8 planner steps and ≤ 8 plan steps, so «заведи 12
предметов» cannot fit; every new tool grows the fixed planner prefix (≈ 12.5k tokens per step today), and cache reads
are charged to the tester's quota as ordinary tokens (ADR-0023) and count in the 60k input budget of a request; and
catalog 9 is reserved for chats (ADR-0028, TASK-244/245). The owner also found a missing feature: an assignment
cannot be marked done.

## Decision

### 1. Definition of done
- A user-facing feature is done only when the agent can do it too, or the task records why it stays UI-only (one of
  the reasons below). The task template has a line «Агент: <tool, class> | только UI: <reason>»; the reviewer checks
  it; `docs/agent-parity.md` is updated in the same task.
- **UI-only reasons** (closed list): a secret is typed (Hub key, mail password, Google client secret); a consent
  page outside the app (Google OAuth); a native open/save dialog; a mail server or an address typed in the connection
  settings («Подключения»); the student's own text in an editor; control of the agent itself (Да, Стоп,
  Продолжить…); pure view state. For such rows the agent may still show the page's own button (`offer_button`, §3).

### 2. Safety classes
- **Owner's rule (2026-10-28): as few «Да» as possible.** A «Да» is asked only for what is irreversible, external,
  expensive or shows a value the model typed (§8). Everything else runs at once with «Отменить».
- **R** — read; free unless paid.
- **U** — local SQLite or artifact-store change with a typed compensation; the agent runs it at once and shows
  «Отменить». The ADR-0021 criterion stays: no network beyond the Hub, no user-visible file, no e-mail, no hard delete.
  A soft delete with «Вернуть» (assignment, note, material, subject) is U. **One exception (О-7 а):** «останови
  запись» runs at once and has no compensation; the result line offers «Начать запись» (a new recording).
- **C** — the agent only creates a card; the student's click runs it in `input/wails`. Used for: irreversible
  (hard delete), external (e-mail, Google writes other than C-4, downloads, sign-out), deleting an assignment that has
  a Google event (О-6 б), a value the model typed for a teacher, a call link or the profile (О-4 в), and **batches
  the desktop builds** (§4). C stays out of `input/agent` (no `Confirm/Execute` method there).
- **UI** — card only. New: `offer_button`.
- **Paid work (О-1 а):** unchanged ADR-0021 rule — one cheap paid job (≤ $0.05) runs at once, an expensive one or a
  second one needs the plan «Да».
- **C-4 stays (О-2 а):** a deadline the agent changes updates the existing Google event without asking.

### 3. `offer_button` (class UI)
- Arguments: `action` (closed enum: open_material, open_call_link, copy_call_link, open_summary, open_lecture_text,
  open_transcript, save_as, google_sign_in, google_sync, download_model, sound_check, open_help, undo_action,
  start_recording) and the one typed ID the action needs (provenance-checked).
- The desktop renders the card from a template with the same button the page has; the click calls the same binding.
  The model never supplies a URL, path, address or label for it. It is the agent's answer to «открой», «сохрани»,
  «войди в Google».

### 4. Plans and budget
- **Batch of one kind → a desktop-built proposal** (class C): one tool call without item arguments
  (`propose_subjects_from_schedule`), the owning use case computes the items, the card lists them from local data
  with per-row choices, one «Да» applies the checked rows through `input/wails`, one item per transaction, idempotent
  by a derived ID, a batch line with «Отменить всё». No planner step, write or token per item; nothing of the items
  goes to the planner (ADR-0022).
- **Plan size (О-9 в): no separate step limit, only the quota.** Approved plan steps are counted against the plan,
  not `MaxWrites`; `MaxWrites` (6) keeps bounding unplanned writes; a plan has no write limit of its own.
  **Technical guards** against runaway loops, not product limits: `MaxPlanSteps` = 200 steps per plan; while an
  approved plan has unused steps the request may take up to `MaxPlanRunSteps` = 60 planner steps and
  `MaxPlanInputTokens` = 1 000 000 input tokens; outside a plan `MaxSteps` (8) and `MaxInputTokens` (60k) stay. The
  Hub `show_plan` schema of catalog ≥ 10 allows 200 steps and its arguments up to 32 KiB (8 KiB for other tools).
  Each planner step stays a quota reservation on the Hub.
- **Plan runner (wave 11, not before):** after «Да» the desktop runs the approved steps itself, in order, filling
  deferred IDs from earlier steps (a plan runner in `input/agent` over `cases/agentturn`; the rules stay in
  `thread`/`actionpolicy`); the Hub gets the results as a new `AgentTurn.plan_run` (catalog ≥ 11) and one planner
  step reports. A failed step stops the rest («Продолжить» resumes; each step still runs at most once). Until then
  the planner executes approved plans within the guards above.
- C steps inside a mixed plan: Open О-10. Until then a plan may list a C step, which shows its own «Да» card.

### 5. Catalog versioning
- Catalog/panel 9 stays exactly ADR-0028 (chats, same tools as 8). Parity tools ship in **waves**, one catalog per
  wave, never one per tool: **10** = §10 below; **11** = §11 below; later waves the same way. Catalogs are additive:
  N = N−1 plus the wave (removals as `generate_draft`, catalog 4).
- Order per wave: domain + use case + binding tasks first (each usable from the UI); a desktop contract task declares
  the wave's tool specs and policy rules (not served yet) so the dispatch tasks run in parallel on disjoint files;
  then the Hub task (panel vN: `tools.json`, `system.md`, schema, golden), deployed before the desktop; then the
  desktop switch task (`CatalogVersion`, golden test, eval level 1 fixtures); level 2 report on the Hub PR; a security
  review per wave (`docs/process/security-review.md`).
- Wave 10 waits for 9 to be merged (prompt and cache layout are rebased on v9).

### 6. Prompt size
- **Consolidate by entity**: one tool per object with an `action` enum where arguments match
  (`set_setting{setting, value}`; `change_assignment{assignment, action: edit|delete|restore}`;
  `note{action, …}`; `recording{action: start|stop}`), shorter descriptions, shared argument types. Target: wave 10
  adds ≤ 2k tokens.
- **Measure**: agenteval records the prefix tokens per catalog; a catalog PR shows the delta.
- **Groups**: Open О-11. If chosen: a core set always plus groups the desktop selects from the request scope and
  mentions; `AgentStep` gets `tool_groups` (closed enum, part of the seal and the fingerprint); each group set is its
  own cached prefix. Rejected: on-demand loading by a tool call.

### 7. Placement (architecture.md)
- New tools: `domain/agent/values` (catalog), `actionpolicy` (rules), dispatch through narrow interfaces in
  `input/agent` (one file per tool group), compensations in the owning use case with an expected version (preferred)
  or in `input/agent.Actions` within the ADR-0021 deviation.
- Subjects from the schedule: pure domain service `library/services/schedulesubjects` (grouping, obvious vs doubtful,
  kinds per group, derived subject ID); `cases/library` `ScheduleSubjects` / `CreateFromSchedule` /
  `UndoFromSchedule`; a confirm-card kind in `domain/agent/values`; binding in `LibraryService`. §12 of
  `architecture.md` gets the service when the task lands.
- Folders (О-3 а): every subject keeps all six system folders at creation; nothing is hidden.
- Doubtful matches (О-12 б): merged by default in the review; the student can choose «Отдельно» or «Не заводить».
- Done assignments: `coursework/assignment` (§9).
- New ID kinds as needed (`note`, `schedule_group`; `setting` is an enum, not an ID).

### 8. Values the model types (О-4 в, О-5 в, О-8) — owner override
- **Teacher e-mail, call link, profile field.** The owner accepts that such a value goes to the model: the model may
  pass it as a tool argument (wave 11). This overrides ADR-0022 («the planner never sees an address or a link»),
  ADR-0030 («the agent has no tool to set a teacher») and ADR-0032 («no tool to set a link») for **writing** only.
  Still: the desktop validates (the ADR-0030 e-mail rules, `ParseCallLink`, the profile field limits); the tool is
  class C and the «Да» card shows the exact value that will be saved; the value is never logged (only the reason
  code); `AgentBase` still carries no personal data. Reading stored values back to the planner: Open О-14.
- **«Запиши в конспект: …».** The model passes the text as an argument (`note{action: add_text}`, ≤ 4000
  characters, the `reply_text` type). It is saved as a text part of «Мой конспект» — the student's own note, not a
  paratech summary — class U: the result line shows the saved text with «Отменить», no «Да».
- **Chat title.** The agent never renames an existing chat. On the first request of a new chat it may give that
  chat's title (`reply.chat_title`, ≤ 60 characters, plain text, wave 11); the student can rename it later.

### 9. Done assignments («Выполнено»)
- `coursework/assignment`: `MarkDone(now)` and `MarkUndone(now)`, `Done()`, `DoneAt()`; a deleted assignment is not
  marked (ErrDeleted); marking a done one again changes nothing. Storage `assignments.done_at` (one migration).
- A done assignment leaves «Сегодня», «Горит на 7 дней» and the hint counters; it stays in the list of assignments
  (filter «Выполненные», shown struck through with «Вернуть в работу») and in the subject history; it can be un-done.
- Google Calendar: nothing is sent when an assignment is marked done (Open О-13 asks whether the event should go).
- UI: «Выполнено» on the assignment page and as a row action in the list; the result toast has «Отменить».
- Agent: `mark_assignments_done{assignments: ids (1..50), done: bool}`, class U, one write for the whole list, the
  compensation restores each assignment's previous state with its expected version. `list_assignments` gets
  `state: open|done|all` (default open) and `done` in its rows.

### 10. Wave 10 (catalog 10 = 9 + these)
| Tool | Class | Arguments | Notes |
|---|---|---|---|
| `propose_subjects_from_schedule` | C (batch card) | none | §4; card kind `schedule_subjects` |
| `change_assignment` | U; C when it deletes one with a Google event | `assignment` id, `action` edit/delete/restore, `title`, `note`, `due` date, `time`, `subject` id | restore only what this chat deleted (ADR-0029 C-2); card kind `delete_assignment` |
| `mark_assignments_done` | U | `assignments` ids, `done` bool | §9 |
| `list_assignments` (changed) | R | + `state` | rows + `done` |
| `list_notes` | R | `lesson` id or `subject` id | IDs, anchor date and kind, part counters; no titles, no text |
| `note` | U | `action` add_text/add_upload/move/rename/remove, `note` id, `lesson` id, `date`, `upload` id, `text`, `title` | §8; remove = soft, Undo = restore |
| `get_settings` | R | none | the four toggles below as enums |
| `set_setting` | U | `setting` (recording_prompts, ac_power_only, recognition_load, free_work_notice), `value` enum | Undo = old value; later settings join in wave 11 |
| `quota_status` | R | none | remaining share and reset date only |
| `recording` | U (start) / U without Undo (stop) | `action` start/stop | start: Undo = stop and keep the audio; stop: О-7 а |
| `offer_button` | UI | `action` enum, one id | §3 |
| `show_plan` (changed) | — | up to 200 steps, 32 KiB | §4 |

### 11. Wave 11 (catalog 11 = 10 + these)
- C cards with a model-typed value (§8): `set_teacher{subject, role, name, email}` / clear, `set_call_link{subject or
  lesson, link}` / clear, `set_profile_field{field, value}`.
- `reply.chat_title` (§8).
- Library: `move_to_inbox`, `unlink_material`, `remove_material` (U, Undo = restore), `add_alias` (suggestion ID),
  `remove_folder`, `remove_subject` (after TASK-288).
- Sending: time in `propose_send` (after TASK-239), reschedule and cancel cards (after TASK-242).
- Settings added to `set_setting`: calendar update on deadline change (TASK-276), photo OCR off/local (TASK-294).
- Plan runner and `AgentTurn.plan_run` (§4).
- Tickets from a file, «Переписать», «Остановить написание», source switch, default model, archive chat, the action
  journal as R: in wave 11 if their tasks are ready, else wave 12.

## Consequences
- The task template, the reviewer checklist and `docs/agent-parity.md` change now (template) and with each wave.
- Every wave is a Hub-first deployment and a golden update; old catalogs stay served.
- Batches through desktop-built proposals put list logic in use cases and domain services, not in prompts.
- Large approved plans cost more quota until the plan runner lands (each step is a planner step); the guards bound a
  runaway request.
- Teacher e-mails, call links and profile values typed in a request reach the model and the Hub (owner's choice);
  the security review of wave 11 checks validation, the card and the logs.
- The parity table is a living document; a feature task that adds a binding adds its row.

## Answered 2026-10-28
- О-1 (а) cheap paid work at once, expensive through «Да»; minimise «Да». О-2 (а) C-4 stays. О-3 (а) six folders.
  О-4 (в) values as arguments, with validation, a «Да» card showing the value, no logging. О-5 (в) text as an
  argument, class U with «Отменить». О-6 (б) deleting with a Google event only after «Да». О-7 (а) start and stop at
  once. О-8 no rename of an existing chat; a title for a new chat. О-9 (в) no step limit, only the quota and the
  technical guards. О-12 (б) doubtful matches merged by default.

## Open (вопросы владельцу)
- **О-10. Один «Да» на план с отправкой.** Сейчас каждая отправка письма внутри одобренного плана всё равно
  показывает свою карточку «Да». Сделать ли один «Да» на весь план, если карточка плана покажет каждую отправку
  целиком (кому, что, когда)?
- **О-11. Размер подсказки модели.** Каждый новый инструмент агента делает каждый его шаг немного дороже для квоты
  тестера. Оставить все инструменты всегда, но сжатыми, или показывать модели только группу, нужную для текущего
  экрана и просьбы?
- **О-13. Выполненное задание и Google Календарь.** Сейчас при отметке «Выполнено» событие в календаре остаётся как
  есть. Убирать ли его из календаря (и возвращать при «Вернуть в работу»)?
- **О-14. Видит ли модель сохранённые почту, ссылку, профиль.** Сейчас модель может их только записать через карточку
  «Да», а при чтении видит лишь «задано / не задано». Разрешить ли ей читать сами значения, чтобы отвечать на «какая
  почта у лектора?»?
