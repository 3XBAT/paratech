# ADR-0028: Agent chats and the context ring

- **Status:** Accepted (owner 2026-10-27 «делаем»); Open 1–7 answered by the owner 2026-10-27 (below); Open 8–9 wait
- **Date:** 2026-10-26, reworked 2026-10-27
- **Source:** owner 2026-10-26 (chats like Cursor, a ring near the input) and answers of 2026-10-27; research
  `paratech-ops/research/agent-chats-2026-10.md`; ADR-0021 («Threads per subject», superseded here), ADR-0022,
  ADR-0023 (history pairs), ADR-0026; `thread/budget.go` (`Window`). Tasks TASK-203…208, TASK-244…251, TASK-265…273.

## Context
Today there is one thread per subject key (`SubjectID` or `NONE`, `agent_threads.subject_key UNIQUE`). `thread.Window`
sends at most 12 earlier requests as pairs (≤ 24 KiB, ≤ 64 turns per step); older ones are silently forgotten, and one
endless thread mixes topics. Earlier requests already travel only as pairs (student text + a desktop line built from
validated planner texts); tool results never re-enter the history. A planner step reads ≈ 11 000 input tokens, most
of it the fixed prefix; the model window (≈ 200k) is never close. The schema comment says agent steps are purged after
90 days; no code does it yet.

Owner 2026-10-27: one general list of chats, not tied to a subject (one chat may span Матан and Физика); the chat
keeps references, never copies of the knowledge base; window of the last 20 answers; archive only, no delete; an old
chat can be continued; a restart from an earlier message after a warning; the ring changes colour at 70 % and shows
«Начать новый чат» at 90 %. TASK-203…207 built the chats (domain, migration 0040, `cases/agentchats`, bindings, UI).

## Decision

### A chat is a thread, free of subjects
- A chat **is** an `agent/thread` aggregate; no new context or aggregate. The thread has `title`, `archivedAt`,
  `lastActive`; no subject key.
- The subject belongs to the **request**, as the scope does (context chip: screen, subject, lesson, assignment). A chat
  holds requests of any subjects.
- **Current chat**: the unarchived chat with the newest `lastActive`. `Ask` without a chat id goes there (created if
  none); with a chat id, to that chat.
- **New chat** («Новый чат»): an empty current chat is returned instead of a new one. Title «Новый чат» until the
  first request, then the student's first text cut to 40 runes on a word boundary; never model text (hard rule 3).
  **Rename**: 1–80 runes, no control or bidi characters (checked in the aggregate).
- **Archive / restore**, never delete. Archive only when the current request is final or there is none. An archived
  chat can be opened and continued: a new request in it restores it.
- **Switch** at any time; a running request of another chat keeps running, its updates carry its `threadId`. Budget,
  plan, provenance (`seen`) and Undo stay per chat.
- **Hub**: `thread_id` stays opaque, one per chat; seals and kept model turns are bound to it.
- No manual «chat scope» picker (subject checkboxes); only @-mentions (Open 5 → а).

### References, not copies
- History pairs stay as they are: no tool result, file text or lecture text is stored into or re-sent from a chat.
- **Mentions**: the student mentions a subject, a lesson, a material or an assignment in the input («@»).
  `values.Mention{Ref, Label}`: `Ref` is the typed ID; `Label` is the name at mention time, for display only, never
  sent to the Hub (ADR-0022). At most 8 per request, stored with the request (`agent_turns.mentions`).
- **Display** (Open 2 → б): a pill always shows the stored label, also after the object is renamed or deleted. A click
  opens the object's page; a deleted one shows that page's usual «not found» state.
- **To the Hub, IDs only**: additive `AgentScope.mentions` (`repeated AgentMention {kind, id, current}`): the mentions
  of the current request plus the distinct mentions of the earlier requests in the window, ≤ 16. They enter the
  provenance check like scope refs. The agent reads content through its tools by ID.
- Before a step the desktop checks that every mentioned object still exists; a missing one is not sent.

### History window and prompt cache
- `MaxHistoryTurns` 20 earlier answers, `MaxHistoryBytes` 48 KiB, `MaxRequestTurns` 64 (Hub limit).
- The window start moves in **steps of 5** requests, so the history prefix stays byte-identical for up to 5 requests.
- Hub layout from **catalog 9** (panel v9; catalog/panel 7 is long replies TASK-223, 8 is the subject question of
  ADR-0029): tools → system → base (breakpoint) → history summary (if any) and earlier pairs (breakpoint) → scope and
  mentions as a user data block right before the current request → current request (breakpoint on the last block).
  Old catalogs keep their layout.

### Context compaction (Open 4 → б)
- **When**: at `Ask`, before the first step of the new request, when the earlier not dropped requests after the
  summary that the request would carry fill the window (`Fill` ≥ 1 — the ring is full — or `Overflow`). At most one
  compaction per request; never during a step.
- **What**: the oldest 10 such requests (two window blocks; fewer if fewer) plus the previous summary become one new
  summary covering requests 1…`Through`. The pair grouping, limits and alignment are those of `Window`; the plan of
  what to compact is a pure function next to `Window`/`Fill` (`thread.CompactionPlan`).
- **Where (Hub)**: a new Complete operation `OPERATION_TYPE_CHAT_SUMMARY = 9` with the Hub's prompt
  (`pkg/prompts/ops/chat_summary/v1`), its own model alias and output cap in Hub config, a strict reader in
  `hub/internal/domain/llm/services/chatsummary`. `untrusted_data` = JSON `{"summary": "<previous or empty>",
  "pairs": [{"student": "…", "reply": "…"}]}` ≤ 64 KiB; output JSON `{"summary": "…"}`, ≤ 4000 runes, plain text.
  Charged to the tester's quota like any operation; idempotency key `cs:<thread>:<through>` (a retry replays).
- **Storage (desktop)**: the thread keeps one summary (`values.HistorySummary{Through, Text}`, aggregate method
  `Compact(through, text, now)`: `Through` only grows and stays below the current request). Columns on
  `agent_threads`. The text is **model output → untrusted data**: checked like a reply (printable plus line breaks, no
  control or bidi, ≤ 4000 runes), never HTML, never shown as instructions, never a source of provenance IDs, never
  triggers any action (hard rule 3). Not shown to the student (Open 8).
- **Into the window**: a new history turn `AgentTurn.history_summary` (oneof field 5, ≤ 8 KiB, only as the first turn
  and followed by a `student_text`, accepted for catalog ≥ 9). `Window` = summary turn + pairs after `Through`; `Fill`
  counts the summary as one turn and its bytes. Between compactions the history prefix only grows, so it stays cached;
  a compaction changes it about once per 10 requests.
- **UI**: while it runs, `agent:update` of the chat has `compacting: true` and the panel shows the banner «Уплотнение
  контекста…»; after it the update carries the new, lower fill and the ring redraws.
- **Failure** (network, quota, schema): logged by code, the request goes on without a new summary (the window cuts old
  pairs as before, `Overflow`), the next `Ask` tries again.

### Restart from an earlier message (Open 3)
On an earlier message N of the chat the student picks «Начать заново отсюда». A warning offers two choices and
«Отмена»:
- **«Скрыть»**: «Сообщения после этого агент забудет. Сделанные действия не отменяются.» The text of N goes into the
  input (editable); sending drops requests N…last (`dropped_by` = the number of the new request) from the agent's
  context **and from the view**. Only when the current request is final. Nothing is deleted; actions and journal lines
  stay. If N ≤ `summary.Through`, the summary is cleared (the next compaction rebuilds it from the kept texts).
- **«Форк»** (like Claude): a **new** chat with copies of requests 1…N−1 (student text, mentions, scope, frozen reply
  line; no steps, seals, actions or provenance), the summary only if `Through` < N, titled «<title> (fork)» (title cut
  so the whole is ≤ 80 runes). The text of N goes into the input of the new chat, which opens. The original chat stays
  as it is. Allowed at any time (the source is only read).

### What «context used» means (Open 1 → а)
- `thread.Fill(entries, current)` (pure, next to `Window`, skips dropped, counts the summary) returns
  `max(pairs/20, bytes/48 KiB, turns/64)` for what the next request carries, and `Overflow` when an earlier, not
  dropped request is neither sent nor covered by the summary. Desktop only; no Hub field (TASK-209 not needed).

### The ring
- A small ring left of the send button, filled to the percent (≤ 100 %), `role="img"`, hidden text and tooltip
  «Контекст чата: N %».
- < 70 % neutral; 70–89 % accent; ≥ 90 % accent plus the hint «Начать новый чат» with that action; `Overflow` (only
  when a compaction failed) is shown as full. One accent, no red. Level computed in Go.
- Recomputed when a chat opens, when a request ends and after a compaction, never during a step.

### Earlier messages and the current request (Open 7 → а)
- Earlier requests are text only (student text, mention pills, reply line), no card buttons.
- The current request of a chat opened after an app restart shows its live card (for example waiting for «Да»:
  «Да», «Продолжить», «Стоп»): a snapshot binding returns its state in the `agent:update` shape (TASK-272/273).

### Retention (Open 6 → в)
- Kept forever: chats, student texts, mentions, scopes, the frozen reply line of each request (new column
  `agent_turns.reply_line`, written once when the request becomes final) and the summary.
- Purged after 90 days: the technical details of final requests — `agent_steps` (calls, results, seals). `Window`,
  `Fill`, `Earlier`, compaction and fork read only turns, never steps of earlier requests. A cold archive table or file
  is not needed now (texts are small) and can come later without a format change.

## Consequences
- `cases/agentturn`: current chat, `ThreadID`, `Mentions`, `RestartFrom`; writes `reply_line` at the end of a request.
  `cases/agentchats`: list, new, rename, archive, restore, earlier, fork. New use case `cases/agentcompact` (plan,
  Hub call through its own port, `Compact`); `input/agent.Driver` runs it before `Ask` and publishes the banner.
- Migrations: 0040 (chats, done); one more for `agent_turns.reply_line`, the summary columns and
  `agent_threads.forked_from` (informational). Mirrored in `docs/contracts/schema.sql`.
- Hub panel v9 + desktop catalog 9: mentions, «subject of the request», the cache layout, the summary turn.
  `OPERATION_TYPE_CHAT_SUMMARY` is additive (buf breaking green).
- The summary carries text derived from earlier untrusted pairs; an injection in an old request can persist in it as
  data, the same exposure as the pairs today. Hub and desktop parts get an adversarial security review.
- Compaction costs one cheap Hub call per ≈ 10 requests in a long chat; agenteval records `cache_read` before/after.

## Open (owner)
Answered 2026-10-27: 1 — (а) the share of the conversation the agent still remembers; 2 — (б) the name at mention
time; 3 — two choices «Скрыть» / «Форк»; 4 — (б) automatic summary with a banner; 5 — (а) only @-mentions; 6 — (в)
texts forever, technical details 90 days; 7 — (а) earlier messages text only.

8. Показывать ли студенту текст сводки ранних сообщений? (а) не показывать — сводка только для агента; (б) свёрнутая строка «Сводка ранних сообщений» в начале чата, раскрывается по клику.
9. Название форка: владелец написал «<название> (fork)», а тексты интерфейса у нас по-русски. (а) оставить «(fork)»; (б) «(форк)»; (в) «(копия)». До ответа в коде — «(fork)», как сказал владелец.
