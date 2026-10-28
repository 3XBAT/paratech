# ADR-0033: Agent parity — everything the student does in the UI, the agent can do

- **Status:** Proposed
- **Date:** 2026-10-28
- **Source:** owner 2026-10-28 («вся работа с приложением может идти полностью через агента»); the audit
  `docs/agent-parity.md`; ADR-0021, ADR-0022, ADR-0023, ADR-0026…0030, ADR-0032; `domain/agent/values/tool.go`
  (catalog 8, 27 tools), `domain/agent/services/actionpolicy/policy.go`, `domain/agent/thread/budget.go`.
- **Amends (when accepted):** ADR-0021 (class C wording, plan budget), ADR-0023 (tool layout of a step).

## Context
About 200 exported binding methods in `input/wails` are UI actions or reads; the agent has 23 tools (8 R, 12 U, 3
C) plus 4 card tools. Most gaps are local
reversible changes (edit or delete an assignment, notes, settings, library moves); some are external or
irreversible; some are UI-only by nature (secrets, OAuth consent, native file dialogs, the agent's own controls).
Three limits block a full agent: a request has ≤ 6 writes, ≤ 8 planner steps and ≤ 8 plan steps, so «заведи 12
предметов» cannot fit; every new tool grows the fixed planner prefix (≈ 12.5k tokens per step today), and cache reads
are charged to the tester's quota as ordinary tokens (ADR-0023) and count in the 60k input budget of a request; and
catalog 9 is reserved for chats (ADR-0028, TASK-244/245).

## Decision

### 1. Definition of done
- A user-facing feature is done only when the agent can do it too, or the task records why it stays UI-only (one of
  the reasons below). The task template gets a line «Агент: <tool, class> | только UI: <reason>»; the reviewer checks
  it; `docs/agent-parity.md` is updated in the same task.
- **UI-only reasons** (closed list): a secret is typed (Hub key, mail password, Google client secret); a consent
  page outside the app (Google OAuth); a native open/save dialog; a server or recipient address the student types in
  the settings; the student's own text in an editor; control of the agent itself (Да, Стоп, Продолжить…); pure view
  state. For such rows the agent may still show the page's own button (`offer_button`, §3).

### 2. Safety classes (unchanged mechanics, sharper wording)
- **R** — read; free unless paid.
- **U** — local SQLite or artifact-store change with a typed compensation; the agent runs it at once and shows
  «Отменить». The ADR-0021 criterion stays: no network beyond the Hub, no user-visible file, no e-mail, no hard delete.
  A soft delete with «Вернуть» (assignment, note, material, subject) is U.
- **C** — the agent only creates a card; the student's click runs it in `input/wails`. Used for: irreversible
  (hard delete, stopping something that cannot resume), external (e-mail, Google writes, downloads, sign-out), and
  **batches the desktop builds** (§4). C stays out of `input/agent` (no `Confirm/Execute` method there).
- **UI** — card only. New: `offer_button`.
- Paid work keeps the ADR-0021 plan threshold until Open О-1 is answered.

### 3. `offer_button` (class UI)
- Arguments: `action` (closed enum: open_material, open_call_link, copy_call_link, open_summary, open_lecture_text,
  open_transcript, save_as, google_sign_in, google_sync, download_model, sound_check, open_help, undo_action, …) and
  the one typed ID the action needs (provenance-checked).
- The desktop renders the card from a template with the same button the page has; the click calls the same binding.
  The model never supplies a URL, path, address or label for it. It is the agent's answer to «открой», «сохрани»,
  «войди в Google».

### 4. Plans and budget
- **Batch of one kind → a desktop-built proposal** (class C): one tool call without item arguments
  (`propose_subjects_from_schedule`), the owning use case computes the items, the card lists them from local data
  with per-row choices, one «Да» applies the checked rows through `input/wails`, one item per transaction, idempotent
  by a derived ID, a batch line with «Отменить всё». No planner step, write or token per item; nothing of the items
  goes to the planner (ADR-0022).
- **Mixed plans (proposed):** after «Да» the desktop runs the approved steps itself, in order, filling deferred IDs
  from earlier steps (a plan runner in `input/agent` over `cases/agentturn`; the rules stay in `thread`/`actionpolicy`).
  Approved writes count against the plan, not `MaxWrites`; `MaxWrites` (6) keeps bounding unplanned and unmatched
  writes. `MaxPlanSteps` grows from 8 to the value of Open О-9 (desktop and the Hub `plan_steps` schema, new catalog).
  `MaxSteps` counts planner steps only; after the plan one planner step (or a template card) reports the result. A
  failed step stops the rest («Продолжить» resumes; each step still runs at most once).
- C steps inside a mixed plan: Open О-10. Until then a plan may list a C step, which shows its own «Да» card.

### 5. Catalog versioning
- Catalog/panel 9 stays exactly ADR-0028 (chats, same tools as 8). Parity tools ship in **waves**, one catalog per
  wave, never one per tool: **10** = gaps P1–P3, P6, P7 of `docs/agent-parity.md` (subjects from the schedule,
  assignment edit/delete, notes, settings, `offer_button`); **11** = the rest that is decided by then; later waves the
  same way. Catalogs are additive: N = N−1 plus the wave (removals as `generate_draft`, catalog 4).
- Order per wave: domain + use case + binding tasks first (each usable from the UI); then the Hub task (panel vN:
  `tools.json`, `system.md`, schema, golden), deployed before the desktop; then the desktop catalog task (`values`
  catalog, `actionpolicy` table, golden test, dispatch in `input/agent`, compensations); eval level 1 fixtures in the
  same task, level 2 report on the Hub PR; a security review per wave (`docs/process/security-review.md`).
- Wave 10 waits for 9 to be merged (prompt and cache layout are rebased on v9).

### 6. Prompt size (proposed: measure, then consolidate, then group)
- **Consolidate by entity**: one tool per object with an `action` enum where arguments match
  (`set_setting{setting, value}`; `change_assignment{assignment, action: delete|restore}`;
  `note{action, …}`), shorter descriptions, shared argument types. Target: wave 10 adds ≤ 2k tokens.
- **Measure**: agenteval records the prefix tokens per catalog; a catalog PR shows the delta.
- **Groups (if the prefix still passes the limit of Open О-11):** a core set always (reads, cards, the most used
  writes) plus groups the desktop selects from the request scope and mentions (library, notes, settings, sending);
  `AgentStep` gets `tool_groups` (closed enum, part of the seal and the fingerprint); each group set is its own cached
  prefix. Rejected for now: on-demand loading by a tool call (an extra step and a cache miss on the whole prefix of
  the request).

### 7. Placement (architecture.md)
- New tools: `domain/agent/values` (catalog), `actionpolicy` (rules), dispatch through narrow interfaces in
  `input/agent`, compensations in the owning use case with an expected version (preferred) or in `input/agent.Actions`
  within the ADR-0021 deviation.
- Subjects from the schedule: pure domain service `library/services/schedulesubjects` (grouping, obvious vs doubtful,
  kinds per group); `cases/library` `ScheduleSubjects` / `CreateFromSchedule`; a confirm-card kind in
  `domain/agent/values`; binding in `LibraryService`. §12 of `architecture.md` gets the service when the task lands.
- New ID kinds as needed (`note`, `schedule_group`, `setting` is an enum, not an ID).

## Consequences
- The task template, the reviewer checklist and `docs/agent-parity.md` change with the first wave.
- Every wave is a Hub-first deployment and a golden update; old catalogs stay served.
- Batches through desktop-built proposals put list logic in use cases and domain services, not in prompts.
- The parity table is a living document; a feature task that adds a binding adds its row.

## Open (вопросы владельцу)
- **О-1. Платная работа без «Да».** Сейчас одна дешёвая платная операция (до ≈ 5 центов: конспект, поиск домашки,
  разбор задания, ответ по лекциям) идёт сразу, без плана. Вы сказали «трата лимита — только после „Да“». Варианты:
  (а) оставить как есть: дешёвое сразу, дорогое через «Да»; (б) любое платное действие только после «Да», кроме
  ответа на вопрос по лекциям; (в) любое платное действие, включая ответ на вопрос, только после «Да».
- **О-2. Google Календарь при смене срока агентом.** Вчера решено: агент меняет срок — событие в Google обновляется
  без вопроса (C-4). Сегодня: «запись в Google Календарь — только после „Да“». Варианты: (а) C-4 остаётся (это
  отражение уже подтверждённого события); (б) агент спрашивает «Обновить событие?» как при ручной правке.
- **О-3. Папки по видам пар.** У каждого предмета уже сразу есть «Лекции», «Семинары», «Лабораторные», «Материалы»,
  «ДЗ», «Билеты». Варианты: (а) так и оставить; (б) показывать пустые «Семинары»/«Лабораторные» только если в
  расписании есть такие пары (папки остаются, меняется показ); (в) заводить эти папки только при наличии таких пар.
- **О-4. Почта преподавателя, ссылка на пару, профиль через агента.** Сейчас модель никогда не видит и не пишет адреса,
  ссылки и личные данные. Варианты: (а) только вручную, агент показывает кнопку «Указать почту/ссылку»; (б) студент
  пишет адрес или ссылку в своём сообщении, приложение само (без модели) достаёт их из текста и показывает карточку
  «Лектор „Матан“: Иванов, ivanov@… — Да»; модель выбирает только предмет и роль; (в) как (б), но модель передаёт адрес
  аргументом.
- **О-5. «Запиши в конспект: …».** Варианты: (а) не делать, текст конспекта пишет только студент в редакторе; (б)
  приложение кладёт в конспект текст сообщения студента как есть (без модели), модель выбирает только пару; (в)
  модель передаёт текст аргументом (может исказить).
- **О-6. Удаление задания с событием в Google.** Удаление убирает событие из календаря; «Вернуть» после синхронизации
  возвращает задание без события. Варианты: (а) агент удаляет сразу с «Отменить», как без события; (б) если есть
  событие — только после «Да».
- **О-7. Запись по просьбе.** Варианты: (а) «начни запись» и «останови запись» агент делает сразу; (б) начать — сразу,
  остановить — только после «Да»; (в) и то и другое только после «Да».
- **О-8. Название чата от агента.** Сейчас название — только слова студента. Варианты: (а) агент может переименовать
  чат словами из просьбы; (б) переименование только вручную.
- **О-9. Сколько шагов в одном плане.** Сейчас до 8 и до 6 изменений за просьбу. Варианты: (а) до 20; (б) до 50; (в)
  без отдельного предела, только квота.
- **О-10. Один «Да» на план с необратимыми шагами.** Например «отправь три работы и поставь сроки в календарь».
  Варианты: (а) «Да» плана — только обратимое, у каждой отправки своя карточка «Да»; (б) один «Да», если карточка плана
  показывает каждую отправку целиком (кому, что, когда) так же, как отдельная карточка.
- **О-11. Размер подсказки модели.** Каждый новый инструмент делает каждый шаг агента дороже для квоты тестера.
  Варианты: (а) все инструменты всегда, но объединённые и короткие; (б) группы инструментов по экрану и упоминаниям,
  когда добавка превысит 2 тыс. токенов; (в) группы сразу, начиная с волны 10.
- **О-12. Сомнительные совпадения в «Завести предметы из расписания».** Варианты: (а) в плане отдельной строкой,
  по умолчанию «Отдельно»; (б) по умолчанию «Объединить»; (в) без выбора по умолчанию, «Да» неактивна, пока не выбрано.
