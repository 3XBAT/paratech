# ADR-0038: The agent can do everything in the app («Агент может всё»)

- **Status:** Proposed (owner instruction 2026-11-10; decisions D-1…D-4 to confirm, Open О-1…О-7)
- **Date:** 2026-11-10
- **Source:** owner 2026-11-10: «Сделай так, чтобы агент мог всё. В рамках программы агент может делать всё. Иногда он
  должен попросить разрешения. Потому что это AI-ассистент, а не просто приложение для ведения своих учебных дел.»
  Trigger: he pasted his timetable (15 rows: day, time, «предмет (преподаватель)», kind, link; one row «очно, ауд. 0209»,
  one link «не подтверждена»; different links for the lecture and the seminar of «Социология культуры» and «Методы ИИ»)
  and asked «внеси все эти ссылки»; the agent answered `cannot_do`: no tool sets a link.
  ADR-0021, ADR-0022, ADR-0030, ADR-0032, ADR-0033, ADR-0034 §5; `docs/agent-parity.md` (2026-11-10).
- **Supersedes:** ADR-0032 «Set and cleared only by the student … The agent has no tool to … set a link» (and the
  TASK-364 addendum «It still has no tool to set it»); ADR-0033 §1 list of UI-only reasons (replaced by §2 below);
  ADR-0033 §11 `set_call_link` / `clear_call_link` (replaced by `set_call_links`, §4); the policy-only «только UI» rows
  of `docs/agent-parity.md` (listed in §3).
- **Amends:** ADR-0032 (call links per lesson kind, addendum 2026-11-10), ADR-0022 (the student's pasted text as an
  argument, §4), ADR-0030 (assignment teacher label, recipient address), ADR-0033 §5/§6 (waves 11 and 12, prefix budget).

## Context
ADR-0033 made parity the definition of done but kept a list of «только UI» rows. Some of them are real limits (a secret,
a browser consent page, a native file dialog); others are only a policy choice made when the agent was new («the
student's own text», «low value», «the link is set only by the student», «the pasted list never goes through the
planner»). The owner now wants the agent to be the main way to use the app; it may ask permission sometimes.
Facts checked for this ADR: the subject keeps one call link (`subject.CallLink`, migration 0050); «Подключиться» on
«Сегодня» and in the popup opens it by subject ID; `list_subjects` shows it to the planner (`call_link`); `offer_button`
`open_call_link`/`copy_call_link` answer `unavailable` (TASK-261 was closed); wave 11 tasks TASK-327…336 are pending
and TASK-328 depends on the closed TASK-259/261; catalog 10 is built on the Hub (TASK-325) and waits for TASK-326.

## Decision

### 1. Rule
Everything the student can do in the app, the agent can do too, with the same use case and the same validation as the
page. A row stays manual only for a reason in §2. The safety shape does not change:
- **Hard rule 3.** The model proposes structured data (IDs, enums, dates, short labels, and the values of §4/§5); the
  core validates it with the same parser as the page (`subject.ParseCallLink`, the ADR-0030 e-mail rules, the profile
  field limits, the ticket list parser); then policy or the student's «Да»; then the adapter acts.
- **Class U** (local, reversible) runs at once with «Отменить». **Class C** is a card; the student's «Да» runs it in
  `input/wails`; `input/agent` still has no Confirm/Execute (ADR-0021). A value the model typed (§4) is always C and the
  card shows the exact value that will be saved. **A batch** is one C card that lists every row that will be applied and
  every row that will not, each with its reason.
- **Secrets are never typed by the agent.** For a secret, a secret's destination or a consent page the agent shows the
  button of the settings page (`offer_button{open_settings, section}`), never a field.
- Paid work, plans, provenance, budget, permissions (ADR-0034 §5) and the C-4 calendar rule are unchanged.

### 2. UI-only reasons (closed list, replaces ADR-0033 §1)
1. **A secret is typed:** Hub key, mail password, Google client ID and secret, the local Hub's provider key and database
   URL (hard rule 4; a value in a chat goes to the Hub and the provider).
2. **A value that decides where a secret or all traffic goes:** SMTP server, port, security and login (the mail
   password is sent there), «Адрес сервера» of the Hub (the Hub key and every request go there). A wrong value leaks a
   secret; the card cannot show the consequence.
3. **A consent page outside the app:** Google sign-in (OAuth in the browser).
4. **A native open/save dialog:** the student chooses the path (ADR-0022 «Paths»). The agent offers the page's button.
5. **The student's review of a model result before it is used:** «Как я понял задание» → «Подтвердить», approving the
   work outline, «Принять» of recognised photo text. The model must not approve its own reading.
6. **The student's standing consent for the agent:** «Разрешения» (ADR-0034 §5). An injected text must not grant itself
   a permission to send.
7. **Control of the agent itself and pure view state:** Да, Стоп, Продолжить, Повторить, Начать заново отсюда, форк,
   новый чат, отозвать вопрос; navigation, filters, scroll, «Скрыть».
8. **A physical check by the student:** the sound check (the student listens).

Rows 1–4 and 8 still get a button through `offer_button` where the page has one.

### 3. Rows that were only policy and now get a tool
Wave per row in `docs/agent-parity.md`. In short: the call link (§4), the teacher and the profile (ADR-0033 §8 as
planned), the recipient address of «Подключения» (C value card, ADR-0030 e-mail rules; it is the address homework goes
to, not a server), tickets from a list the student pastes (§5), «Остановить написание», «Переписать», «Повторить шаг»,
«Спросить по шагу», «Запустить сейчас», «Отменить расшифровку» / «Расшифровать снова», source switch, «Пропустить
пару» in the popup, archive / restore / rename a chat on the student's explicit request (О-4), the action journal as R
and «Отменить» by the agent, the default speech model, deleting a model (C), an ICS file from the panel (C preview),
«Отправлено вручную» / «Отправить ещё раз» (C, hard rule 7: never by permission), moving a filed material, the
teacher text of an extracted assignment (U label), note parts (replace, reorder), copy and save buttons for summaries,
transcripts, lecture texts, drafts and subject export.

### 4. Call links per lesson kind and `set_call_links`
- **Model** (ADR-0032 addendum 2026-11-10): a subject keeps a general link and one link per kind: lecture, seminar,
  lab (`practice` uses the seminar link, as `filing.NoteFolder`). «Подключиться» (Today card, popup, `offer_button`)
  opens the link of the lesson's kind, else the general one, else there is none. A seminar never falls back to the
  lecture link.
- **Tool** `set_call_links{rows}` (class C, card kind `call_links`, catalog 11; replaces `set_call_link` /
  `clear_call_link` of ADR-0033 §11). The MODEL extracts the rows from the student's own message; a row is
  `{subject (id) | subject_name (label), kind (general|lecture|seminar|practice|lab), link (call_link text, ≤ 2048
  bytes, optional), status (link|in_person|unconfirmed|none|clear)}`, 1–40 rows, arguments ≤ 16 KiB.
- **Desktop** (`library/services/calllinkbatch`, pure; `cases/library` `ProposeCallLinks` / `ApplyCallLinks` /
  `UndoCallLinks`): an ID passes the typed provenance check (an unknown ID refuses the call, as `reply.assignments`);
  a name resolves by `subjectmatch` over the names and aliases of the library (exact, then with a trailing «(…)» cut);
  every link is parsed by `subject.ParseCallLink`. Each row gets one outcome: `ready` (new or replaces another link,
  the card shows the old host), `same` (already saved), `clear`, `in_person`, `unconfirmed`, `no_link`, `bad_link`,
  `unknown_subject`, `ambiguous_subject`, `conflict` (two different links for one subject and kind). Duplicate rows with
  the same link merge.
- **Card:** every row with the subject name from local data, the kind, the full link as plain text with the host
  marked, and the old host when it is replaced; ready rows ticked; `unconfirmed` with a valid link and each side of a
  `conflict` shown unticked (the student may tick one); `unknown_subject` with a valid link offers «Создать предмет с
  этой ссылкой» unticked (`CreateSubjectWithCallLink`, the name is the label the card shows); other rows listed below
  with the reason in Russian. One «Да» applies the ticked rows, one subject per transaction, idempotent (same value =
  no change); the batch line has «Отменить всё», which restores each previous value only where the link is still the
  one set (else `UNDO_REFUSED changed`).
- **Planner** gets counters only: `{rows, ready, skipped: {reason: n}}`; never a link back, never a name. Links are
  never logged (only reason codes), as ADR-0032.
- **Reading:** `list_subjects` (catalog ≥ 11) gives `call_links{general, lecture, seminar, lab}` (only the set ones,
  canonical) instead of `call_link`; `offer_button` `open_call_link`/`copy_call_link` take a `lesson` (its subject and
  kind are resolved) or a `subject` and `kind`.

### 5. The student's pasted text as an argument (amends ADR-0022)
ADR-0033 §8 let the model pass the student's dictated note text. The same holds for any text the student pastes into
the request when the tool's parser is the page's parser: a ticket list (`tickets{propose_text, text}`), timetable rows
(`set_call_links`). The text is data: an instruction inside it is never carried out (prompt rule + eval case). Text
from a file, a transcript or a tool result never becomes such an argument (unchanged).

### 6. Waves and the planner prefix (amends ADR-0033 §5, §6)
- **Wave 11 (catalog 11):** ADR-0033 §11 as planned (TASK-327, 329, 330, 331, 333, 335) with `set_call_link` /
  `clear_call_link` dropped, plus `set_call_links`, `list_materials`, `move_material`, more `offer_button` actions
  (copy, save, `open_settings`, `check_mail`, `google_sign_out`, `install_speech`, `save_support_bundle`) and states in
  `get_settings` (Google Calendar, mail, Hub key: set/unset only). Target ≤ +2k tokens over v10.
- **Wave 12 (catalog 12):** tickets, work controls, recordings and jobs, chats and journal, models, ICS from the panel,
  recipient address, reading stored values (О-6), note parts, assignment teacher label, «Сделать задание» route (О-5).
  Target ≤ +2.5k tokens over v11.
- **Wave 13:** what waits for features not built yet: the sending group (send time, reschedule and cancel after
  TASK-239/242, with the «Отправлено вручную» / «Отправить ещё раз» cards in the same task), editing the text of «Мой
  конспект» (TASK-291).
- **Prefix:** v9 ≈ 12.5k tokens per step, v10 ≈ +2.2k, v11 ≈ +1.9k, v12 ≈ +2.3k → ≈ 19k per step. The request limit is
  160k input tokens (`thread.MaxInputTokens`), so an 8-step request on catalog 12 can stop at step 8, and each step costs
  the tester ≈ 50 % more quota than on v9. Every Hub catalog task measures the prefix and stops if v12 > 19.5k (О-3).
- Tasks TASK-328, 332, 334 and 336 are replaced by TASK-438, 441, 442 and 443 (TASK-328 depends on the closed
  TASK-259/261; the others depend on it or on its contract); TASK-327 runs as written except the two call-link tools
  (TASK-434 removes them if they were added).

### 7. Placement (architecture.md)
| Concept | Where |
|---|---|
| Kinds of call link, the join rule | `domain/library/subject`: `CallKind` (general, lecture, seminar, lab), `CallKindOfLesson(lessonKind string)`, value `CallLinks` with `Join(lessonKind) (CallLink, CallKind)`; the aggregate keeps the four links (own copy of the schedule kind codes, ARCH-11, as `filing.NoteFolder`) |
| Batch of model-typed links | domain service `domain/library/services/calllinkbatch` (rows → outcomes, merge, conflicts, diff against current links; uses `subjectmatch`); use case `cases/library` (`ProposeCallLinks` with a token over the content, `ApplyCallLinks`, `UndoCallLinks`) |
| Card kind `call_links`, row type `call_link_rows` | `domain/agent/values` (wave 11 file); `actionpolicy`: C |
| Dispatch, confirm | `input/agent/calllinks.go`; `input/wails/agentconfirm_calllinks.go` |
| «Подключиться» by kind | `cases/todayfeed` and `cases/recordingprompt` read `subject.CallLinks` through their existing ports (port result type changes) and call `Join` |
| Wave 12 groups | one `input/agent/<group>.go` file per group, ports on the owning use cases (ARCH-42) |
No new context, aggregate or ARCH rule. `architecture.md` §12 `library` row names `calllinkbatch` when TASK-435 lands.

## Consequences
- The parity table loses most «только UI» rows; the reviewer checks the template line «Агент:» against §2.
- One migration (next free number at push): `subjects.call_link_lecture`, `call_link_seminar`, `call_link_lab`
  `TEXT NOT NULL DEFAULT ''`; `subjects.call_link` stays the general link, so nothing is copied.
- Links, teacher addresses, profile values and pasted lists the student types reach the Hub and the provider (owner's
  choice, as ADR-0033 §8). The security review of waves 11 and 12 checks the parsers, the cards, the logs and the
  injection cases (a timetable cell or a ticket line with an instruction).
- More tools make each planner step more expensive (§6); the owner decides О-3.

## Decisions to confirm (taken by the architect from the instruction)
- **D-1.** The instruction is the owner's choice to switch catalog 10 on (the +~2.2k tokens per step of TASK-326).
  TASK-326 itself is unchanged and runs first.
- **D-2.** `practice` uses the seminar link; an unknown or empty kind uses the general link; no fallback between kinds.
- **D-3.** Unconfirmed links are not saved unless the student ticks the row on the card.
- **D-4.** A subject not in «Предметы» is not created silently: the card offers «Создать предмет с этой ссылкой»
  unticked.

## Open (вопросы владельцу)
- **О-1. Куда «Добавить ссылку» на карточке пары сохраняет ссылку.** На карточке лекции: (а) по умолчанию «для лекций
  этого предмета», рядом выбор «для всех пар предмета» (рекомендую); (б) всегда «для всех пар», а ссылки по типу —
  только на странице предмета.
- **О-2. Ускорить ссылки?** Сейчас ссылки агентом появятся вместе со всей волной 11 (≈ 12 задач после TASK-326). (а)
  оставить одну волну (рекомендую: вписать ссылки по типу пары вручную можно раньше, после TASK-433); (б) отдельный
  маленький каталог только для ссылок, преподавателей и профиля, остальное — следующим.
- **О-3. Цена подсказки модели.** К каталогу 12 каждый шаг агента будет ≈ 19 тыс. токенов вместо 12,5 тыс. сейчас (+50 %
  квоты тестера на шаг). (а) показывать модели только группу инструментов по экрану и просьбе (ADR-0033 О-11);
  (б) оставить все инструменты всегда; (в) считать в квоте повторно прочитанный из кеша префикс по цене кеша (1/10),
  как платит сам Hub (рекомендую: просто и честно; нужен отдельный ADR к ADR-0008/0023).
- **О-4. Переименование чата.** Раньше: агент никогда не переименовывает существующий чат. Теперь: (а) только по явной
  просьбе студента «переименуй чат в …», с «Отменить» (рекомендую); (б) никогда, как раньше.
- **О-5. «Сделай задание» у агента.** Кнопка «Сделать задание» (TASK-411) сама выбирает короткий ответ или работу под
  ключ. (а) агент идёт тем же путём, короткий ответ — без плана, если дёшево (рекомендую); (б) агент только «под ключ»,
  как сейчас.
- **О-6. Видит ли модель сохранённые почту преподавателя и профиль** (ADR-0033 О-14). (а) да, по прямому вопросу,
  через инструмент чтения, не в каждом шаге (рекомендую); (б) только «задано / не задано».
- **О-7. Учебная запись из панели.** Импорт старой записи сейчас только через диалог. Принимать аудиофайл, брошенный в
  панель агента, как запись (звук остаётся на компьютере)? (а) позже, отдельной функцией (рекомендую); (б) в волне 12.
