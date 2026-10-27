# ADR-0028: Agent chats and the context ring

- **Status:** Accepted (owner 2026-10-27 «делаем»); the items under «Open» wait for the owner
- **Date:** 2026-10-26, reworked 2026-10-27
- **Source:** owner 2026-10-26 (chats like Cursor, a ring near the input) and answers of 2026-10-27; research
  `paratech-ops/research/agent-chats-2026-10.md`; ADR-0021 («Threads per subject», superseded here), ADR-0022,
  ADR-0023 (history pairs), ADR-0026; `thread/budget.go` (`Window`). Tasks TASK-203…209, TASK-244…251.

## Context
Today there is one thread per subject key (`SubjectID` or `NONE`, `agent_threads.subject_key UNIQUE`). `thread.Window`
sends at most 12 earlier requests as pairs (≤ 24 KiB, ≤ 64 turns per step); older ones are silently forgotten, and one
endless thread mixes topics. Earlier requests already travel only as pairs (student text + a desktop line built from
validated planner texts); tool results never re-enter the history. A planner step reads ≈ 11 000 input tokens, most
of it the fixed prefix; the model window (≈ 200k) is never close. Agent history (`agent_steps`) is purged after 90 days.

Owner 2026-10-27: one general list of chats, not tied to a subject (one chat may span Матан and Физика); the chat
keeps references, never copies of the knowledge base; window of the last 20 answers; archive only, no delete; an old
chat can be continued; a restart from an earlier message drops what follows from the agent's context after a
warning; the ring changes colour at 70 % and shows «Начать новый чат» at 90 %.

## Decision

### A chat is a thread, free of subjects
- A chat **is** an `agent/thread` aggregate; no new context or aggregate. The subject key leaves the thread
  (`thread.Key`, `KeyOf`, `SubjectKey`, `NoSubject` as a thread key go). The thread gains `title`, `archivedAt`,
  `lastActive`.
- The subject belongs to the **request**, as the scope already does (context chip: screen, subject, lesson,
  assignment). A chat holds requests of any subjects; there is no «scope subject must match the chat» rule.
- **Current chat**: the unarchived chat with the newest `lastActive`. `Ask` without a chat id goes there (created if
  none); with a chat id, to that chat.
- **New chat** («Новый чат»): an empty current chat is returned instead of a new one. Title «Новый чат» until the
  first request, then the student's first text cut to 40 runes on a word boundary; never model text (hard rule 3).
  **Rename**: 1–80 runes, no control or bidi characters (checked in the aggregate).
- **Archive / restore**, never delete (journal lines reference `thread_id`). Archive only when the current request
  is final (`DONE`, `FAILED`, `CANCELLED`, `STOPPED`) or there is none, else `ErrInvalidTransition`. Archived chats
  are listed under «Архив». An archived chat can be opened and continued: a new request in it restores it
  («старый чат можно продолжить»).
- **Switch** at any time; a running request of another chat keeps running, its updates carry its `threadId`. Budget,
  plan, provenance (`seen`) and Undo stay per thread, so per chat.
- **Hub**: `thread_id` stays opaque, one per chat; seals and kept model turns are bound to it.

### References, not copies
- History pairs stay as they are: no tool result, file text or lecture text is stored into or re-sent from a chat.
- **Mentions**: the student mentions a subject, a lesson (lecture), a material or an assignment in the input («@»).
  `values.Mention{Ref, Label}`: `Ref` is the typed ID; `Label` is the name shown at mention time, for display only,
  never sent to the Hub (ADR-0022). At most 8 per request, stored with the request (`agent_turns.mentions`).
- **To the Hub, IDs only**: new additive field `AgentScope.mentions` (`repeated AgentMention {kind, id, current}`):
  the mentions of the current request plus the distinct mentions of the earlier requests in the window, ≤ 16. They
  enter the provenance check like scope refs. The agent reads content through its existing tools by ID.
- Before a step the desktop checks that every mentioned object still exists; a missing one is not sent (its display:
  Open 2).

### History window and prompt cache
- `MaxHistoryTurns` 12 → **20** earlier answers; `MaxHistoryBytes` 24 → **48 KiB** so 20 typical pairs fit;
  `MaxRequestTurns` 64 unchanged (Hub limit).
- The window start moves in **steps of 5** requests (aligned on the chat's request count): 16–20 earlier answers are
  sent and the history prefix stays byte-identical for up to 5 requests in a row. Byte and turn limits may still cut
  one more step.
- Hub layout from catalog 8: tools → system → base (breakpoint) → earlier pairs (breakpoint) → scope and mentions as
  a user data block right before the current request → current request (breakpoint on the last block). Today the
  scope sits in `system` and every change of the chip invalidates the history cache. Old catalogs keep their layout.
- History retention vs «only archive»: Open 6.

### Restart from an earlier message
- On an earlier message of the chat (request N) the student picks «Начать заново отсюда»; a warning says «Сообщения
  после этого агент забудет. Сделанные действия не отменяются.»; the text of N goes into the input (editable);
  sending drops requests N…last from the agent's context and appends the new request. Only when the current request
  is final.
- Nothing is deleted: dropped requests get `dropped_by` = the number of the new request. `Window` and `Fill` skip
  them; actions and journal lines stay; provenance keeps IDs already seen (real local IDs). This storage serves every
  answer to Open 3.

### What «context used» means (Open 1)
- `thread.Fill(entries, current)` (pure, next to `Window`, same grouping, skips dropped) returns
  `max(pairs/20, bytes/48 KiB, turns/64)` for the earlier requests the next request carries, and `Overflow` when an
  earlier, not dropped request is no longer sent. `Overflow` drives the hint in every variant.
- Ring percent: variant (а) `Fill` (desktop only); variant (б) last-step `usage.input_tokens` (cache included) against
  the model window — needs `AgentStepResponse.context_window_tokens` from the Hub (TASK-209).

### The ring
- A small ring left of the send button, filled to the percent (≤ 100 %), `role="img"`, hidden text and tooltip
  «Контекст чата: N %».
- < 70 % neutral (secondary colour); 70–89 % accent colour; ≥ 90 % accent plus the hint «Начать новый чат» under the
  input with that action. One accent, no red. Level computed in Go, not in the UI.
- Recomputed when a chat opens and when one of its requests ends, never during a step. Full ring / overflow: Open 4.

## Consequences
- `cases/agentturn`: `ThreadRepository.GetByKey` becomes «current chat»; `AskInput` gets `ThreadID` and `Mentions`.
  New use case `cases/agentchats` (list, new, rename, archive, restore, earlier requests, restart).
- Migration (next free desktop number): rebuild `agent_threads` without `subject_key`, with `title`, `archived_at`,
  `last_active_at`; each existing thread becomes a chat titled with its subject's name («Без предмета» for `NONE`);
  `agent_turns.mentions`, `agent_turns.dropped_by`. Mirrored in `docs/contracts/schema.sql`.
- Hub panel v8 + desktop catalog 8 (after v7 of ADR-0029 addendum 2): mentions, «subject of the request» instead of
  «subject of the thread» in the prompt (also the topic rule of ADR-0025 TASK-187), the cache layout.
- The journal takes the subject of a line from its request scope and shows the chat title.
- No change to the per-request budget. Proto comments «one thread per subject» are updated.
- Prompt-cache gain across requests is expected, not measured; agenteval records `cache_read` before/after.

## Open (owner)
1. Что показывает кольцо? (а) долю разговора, которую агент ещё помнит (окно 20 ответов; бесплатно, без изменений Hub — дешевле); (б) реальные токены последнего шага против окна модели (нужно поле от Hub, TASK-209; по замерам почти всегда 5–15 %).
2. Упомянутый объект удалён или переименован: (а) серая пилюля «удалено», агенту не передаётся; (б) показывать название на момент упоминания; (в) при переименовании — новое имя, при удалении — (а). Дешевле всего (б).
3. Перезапуск с сообщения — что видно из «забытых» сообщений: (а) они скрываются из чата (хранятся, не видны); (б) остаются скрытой веткой, к которой можно вернуться; (в) создаётся новый чат-копия до этого места, старый не меняется. Дешевле всего (а).
4. Кольцо заполнено (агент уже не видит ранние сообщения): (а) агент просто забывает старое, кольцо и подсказка это показывают; (б) автосводка старых сообщений (платный вызов Hub); (в) предложить новый чат со сводкой этого. Дешевле всего (а).
5. Нужен ли ручной выбор «области чата» (галочки предметов) кроме @-упоминаний? (а) нет; (б) да. Дешевле (а).
6. История агента очищается через 90 дней, а чаты «только архив»: (а) хранить всё бессрочно; (б) оставить 90 дней (старые сообщения чата пропадут); (в) бессрочно — тексты студента и строки ответов, вызовы и результаты инструментов — 90 дней. Дешевле всего (а).
7. При переключении прежние просьбы — только текст (вопрос и строка ответа), кнопки только у текущей: (а) достаточно; (б) нужны прежние карточки.
