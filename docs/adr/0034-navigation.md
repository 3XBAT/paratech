# ADR-0034: Navigation of the main window, settings and permissions

- **Status:** Proposed
- **Date:** 2026-11-03
- **Source:** owner 2026-11-02 («да» to the UX audit plan: quick fixes + an architect pass on navigation; the UI is too
  complex, usability first: no dead ends, fewer clicks, fewer «Да»); owner answer О10 = б 2026-11-02 (each external
  send keeps its own «Да», the first time with «Больше не спрашивать», a settings section of such permissions);
  `paratech-ops/research/ux-audit-2026-10-28.md` (audit items are cited as «А-n.m»), `paratech-ops/research/ux-guidelines.md`;
  current master (after TASK-316, 337–342).
- **Amends:** ADR-0021 and ADR-0033 §2 (class C: a standing permission the student set may stand in for the click,
  §5), ADR-0033 О-10 (answered), ADR-0029 addendum 2 (its setting moves into «Разрешения»), PRD AC-14 wording (§5).

## Context
The audit found no visible model «пара → запись → конспект → задание»: one thing lives in two or three places under
different names («Предметы» and «База знаний»; three screens of one lesson; «Записи и задачи» reachable only from a
button on «Сегодня»), the rail has icons without labels, and the most frequent action («Начать запись») is two levels
deep. Since the audit TASK-339 gave every file row its actions, TASK-341 made «Предметы» open the tree with the subject
and «Входящие» as the first tree item (with «Добавить файл» and «+» per folder), TASK-338 explained and relaxed the
delete lock of an assignment, TASK-337 removed the «Ответ» caption, TASK-340 streams the reply, TASK-342 removed the
free-work card and its setting, TASK-316 added «Выполнено».

## Decision

### 1. Sections (the rail)
The rail shows **icon + label under it** (width 72 px), in two groups:

| Top | Bottom |
|---|---|
| Сегодня · Записи · Предметы · Задания · Расписание | (busy pill) · Журнал · Настройки |

- **«Записи»** becomes a section of its own (route `recordings`); the hidden «Записи и задания» mode of «Сегодня» goes.
- **«Журнал»** moves to the bottom group (rarely needed, А-11.1).
- **Busy pill** above «Журнал»: «● 12:40» while recording, «⟳ 2» while jobs run; click opens «Записи» (А-0.13).
- No new top-level section is added for «База знаний» or «Билеты» (§3).

### 2. What lives where
| Section | Content | One primary action |
|---|---|---|
| Сегодня | pairs of the day (each row opens its lesson or offers «Записать»), «Нужно ваше решение», «Сроки на неделе», «Итоги дня» at the end; first-run checklist «Начало работы» instead of empty blocks | «Записать пару» (during a recording: «● Идёт запись 00:41 · Остановить») |
| Записи | recordings with their chain; «Фоновая обработка» folded; import of an old recording in the head | «Начать запись» |
| Предметы | tree (Входящие, semesters, subjects, folders) + the open subject; a search field in the head searches all lectures and notes | «Добавить файл» (in a subject: into it) |
| Subject page | head with name, teachers, «Билеты к экзамену»; tabs **Пары · Файлы · Задания** | per tab: «Импортировать запись» / «Добавить файл» / «Добавить задание» |
| Lesson page (one screen) | tabs **Мой конспект · Конспект · Текст лекции · Расшифровка · Задания**; «Исправить предмет или тип пары» in «⋯» | «Скопировать» on «Конспект» |
| Задания | list grouped «Просрочено / На этой неделе / Позже / Без срока»; assignment page | «Добавить задание»; on the page: §7 В2 |
| Расписание | as today (week view is a later task, out of this ADR) | «Выбрать файл…» |
| Журнал | as today, fewer filters visible | none |
| Настройки | §4 | none |

- **One lesson screen** (route `lesson` = the lesson key of the knowledge base, plus a tab) replaces the three today
  (KnowledgeLessonPage, the SummaryPage and TranscriptPage overlays). Every «Открыть конспект / расшифровку / текст
  лекции» goes there with its tab. Tab order keeps the owner's decision «Мой конспект» first; the page opens on «Мой
  конспект» when it has parts, else on «Конспект» when it is ready, else on the first tab that has content.
- **Back** is a breadcrumb left above the title (`Предметы / Матанализ / Пара 21.10`), clickable links, the same on every
  nested screen; Alt+← keeps walking the route history (TASK-201). Deleting a recording from its lesson goes back where
  the student came from.
- **«Входящие»** = everything not sorted yet: files (TASK-339) and lessons whose subject is not a library subject
  («Пары без предмета», each with «Указать предмет»).
- **Every list row opens its object** (whole row) and has «⋯» for the rest; dangerous actions only in «⋯» or in a
  dialog, never as a red button next to a useful one.

### 3. «База знаний» and «Билеты»
- (Recommended, owner question В1.) «База знаний» stops being a screen: its subject view becomes the tab «Пары» of the
  subject page, its search becomes the search field of «Предметы» (results replace the right side, route
  `subjects` + `search`), answer sources open the lesson page. Export of a subject («Сохранить все конспекты одним
  файлом») moves to the «⋯» of the tab «Пары».
- «Билеты» stays a page but is opened from the subject head («Билеты к экзамену») with the subject chosen; the
  «База знаний»/«Билеты» buttons leave the head of «Предметы».

### 4. Settings
Left menu, top to bottom; nothing deeper than one level:

| Item | Content |
|---|---|
| Основные | Запись, Обработка, Фото конспектов (as today) |
| **Разрешения** | §5 |
| Подключения | ключ доступа paratech, почта, Google; «Адрес сервера» under a folded «Дополнительно» |
| Профиль | as today |
| Оформление | as today |
| *Дополнительно* (caption) · Модели речи · Диагностика | as today, below a divider |

The over-line «Изменения сохраняются сразу» goes (А-0.9); «Проверить звук» also stays reachable from the start of a
recording and the checklist.

### 5. «Разрешения» (О10 = б)
**Kinds** (closed set, `domain/settings/settings`):

| Kind | Covers | Row text |
|---|---|---|
| `send_to_teacher` | confirm cards of `send_homework_email` and `send_work_email` (agent or the «Отправить преподавателю» flow) | «Отправлять работы преподавателю» |
| `deadline_to_calendar` | confirm cards of `create_deadline_event` | «Добавлять сроки в Google Календарь» |
| (existing) calendar on due change | TASK-276…280, three values | «Обновлять событие при смене срока»: спрашивать / обновлять сами / не обновлять |

Mode per kind: `ask` (default) | `allow`, plus `offered` (the first-time checkbox was already shown and answered).

**Always ask, never a permission:** a send whose card asks to choose the teacher (`ReasonChooseTeacher`), «Отправить
ещё раз» after an UNKNOWN delivery (hard rule 7), deleting an assignment that has a Google event (О-6 б), hard deletes,
model-typed values (ADR-0033 §8), expensive paid work (ADR-0021), the send dialog where the student picks the format.

**Flow.**
1. First card of a kind with `ask` and `offered = false`: the card shows the checkbox «Больше не спрашивать» (applies
   only with «Да»). Any answer sets `offered`; «Да» with the box ticked also sets `allow`.
2. A card of a kind with `allow`: the core confirms it at once with exactly the facts the card would show (recipient,
   subject line, format, deadline) and executes it through the normal path (validation, recheck, idempotent job,
   delivery reconciliation). The card shows the outcome: «Письмо отправлено · без вопроса (Настройки → Разрешения)»
   with the link «Спрашивать снова».
3. «Настройки → Разрешения» lists the kinds with «Спрашивать / Не спрашивать» and one line on what «Не спрашивать»
   means; the TASK-280 choice lives here too.

**Placement.**
- `domain/settings/settings`: `PermissionKind`, `PermissionMode`, `Permission{mode, offered}`, setters with the version
  bump (like the other settings); storage columns in `settings` (one goose migration).
- `domain/coursework/services/autoconfirm` (pure): `Decide(mode, kind, needsChoice) → Ask | Confirm`; its own mode enum
  (ARCH-11, as `eventrefresh`); table test.
- `domain/coursework/proposal`: records that a confirmation came from a permission (`ByPermission`), for the card and
  the journal; storage column.
- `cases/preferences`: `SetPermission(kind, mode)`, `MarkPermissionOffered(kind)`.
- `cases/homeworkflow`: `ConfirmByPermission(ctx, id)` — reads the mode through a narrow port, calls `autoconfirm`,
  confirms with the trusted facts, executes; idempotent (a second call is a no-op).
- `input/wails`: the confirm binding takes `dontAskAgain`; the card read returns `offerDontAsk` and the outcome; the
  `ConfirmByPermission` is called only from `input/wails`, right after a proposal that will show a confirm card exists:
  in the forwarder of agent updates (a new confirm card) and in the bindings that create a send proposal from the
  student's button. `input/agent` still has no Confirm/Execute (ADR-0021). `SettingsService.Permissions/SetPermission`.
- **The agent cannot change permissions**: `set_setting` never accepts them (ADR-0033 §1 reason «control of the agent
  itself»); no tool reads them either.
- The «Сегодня» post «Да, в календарь» stays a one-click button (the click is the action, no second question).

### 6. Words (glossary)
`docs/design/glossary.md` is the source: «пара» (schedule event), «запись», «Конспект» (paratech) and «Мой конспект»
(the student's, owner's term, kept), «Текст лекции», «Расшифровка» (never «распознавание» in UI), «задание» (no
«ДЗ»/«домашка» in texts; the system folder «ДЗ» keeps its name), «работа под ключ», waiting states «Ждёт интернет /
Ждёт пополнения минут / Ждёт зарядки», buttons are verbs, confirmations «Да, <глагол>» / «Не <глагол>».

### 7. Migration from today's UI (each step a task, see tasks.json TASK-344…362)
0. Quick fixes that do not depend on the new structure: words (344), «Сегодня» (345), «Задания» and «Журнал» (346),
   «Записи», «Предметы» and dialogs (347).
1. Rail with labels, «Записи» section, «Журнал» down, «Записать пару» on «Сегодня» (348); busy pill (349).
2. Breadcrumbs (350).
3. One lesson screen: route and page (351), all entry points and the overlays removed (352).
4. Subject page tabs and «Билеты к экзамену» (353); «База знаний» folded into «Предметы» (354, after В1).
5. Agent panel: context chip of the open object, suggestions in the empty state, input at the bottom (355).
6. Settings structure (356); first-run checklist on «Сегодня» (357).
7. Permissions: domain and storage (358), use cases (359), binding and trigger (360), UI (361); independent of 0–6
   except that 361 follows the settings menu (356).
8. Assignment page: one entry «Сделать задание» (362, after В2).

Each step leaves the app fully usable; no step removes a way to an object before its new way exists.

## Consequences
- Fewer places per object: a lesson has one screen, a subject one page, recordings one section.
- Routes grow (`recordings`, `lesson`, `search`); the route history covers them, so Alt+← and mouse buttons work.
- `docs/agent-parity.md`: navigation is pure view state (UI-only); permissions are «control of the agent itself»
  (UI-only, never a tool).
- **Security.** With `allow`, an agent proposal (including one an injected text provoked) sends the student's own
  stored work to the teacher of the subject without a click. The recipient, attachment and subject still come only
  from trusted local data and are rechecked; the model can neither set the permission nor choose the recipient; every
  such send is in the journal and on the card. The owner accepted the responsibility (О10 = б). The permission tasks
  get the adversarial review of `docs/process/security-review.md`.
- **PRD.** AC-14 «Sending homework requires explicit user confirmation» and §13.3 «policy/user confirmation» are read as:
  a per-card «Да» or a standing permission the student set by an explicit click; the PRD line is updated when this ADR
  is Accepted.
- ADR-0029 addendum 2: its setting is shown in «Разрешения» (no behaviour change).
- Dropped audit items (already fixed or against an owner decision): see the report of the architect pass (2026-11-03)
  and the Notes of TASK-344.

## Open (вопросы владельцу)
- **В1. «База знаний».** Сейчас пары предмета и поиск лежат на отдельном экране «База знаний», а файлы того же
  предмета — в «Предметах». Как сделать?
  (а) убрать отдельный экран: пары — вкладка «Пары» на странице предмета, поиск — поле в шапке «Предметов»
  (рекомендую); (б) оставить отдельный экран только для поиска; (в) оставить как есть.
- **В2. Как получить решение задания.** На странице задания три пути: «Быстрый черновик», «Сделать под ключ» и
  «Написать работу» внутри него. Как сделать?
  (а) одна кнопка «Сделать задание» с выбором «Короткий ответ (текстом, ~1 мин)» или «Работа под ключ (файл Word или
  PowerPoint, с вопросами, дороже)»; черновики, версии и письма — свёрнуты ниже (рекомендую); (б) оставить оба блока,
  «под ключ» первым, «Быстрый черновик» свернуть; (в) оставить как есть.
- **В3. «Больше не спрашивать» для писем.** Разрешение действует (а) на все предметы сразу (рекомендую, проще);
  (б) отдельно для каждого предмета.
