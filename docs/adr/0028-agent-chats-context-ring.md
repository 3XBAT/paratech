# ADR-0028: Чаты агента и индикатор контекста

- **Status:** Proposed (backlog; not scheduled before current work, owner 2026-10-26)
- **Date:** 2026-10-26
- **Source:** owner's request of 2026-10-26 (chats like Cursor, a ring near the input); ADR-0021 (threads per subject),
  ADR-0023 (AgentStep, history pairs), ADR-0026; `thread/budget.go` (`Window`), TASK-172, TASK-182, TASK-183.

## Context
Today there is one thread per subject key (`SubjectID` or `NONE` «Без предмета», `agent_threads.subject_key UNIQUE`).
A long conversation never overflows the model: `thread.Window` sends at most 12 earlier requests as pairs, ≤ 24 KiB,
≤ 64 turns per step. Instead the agent silently forgets the oldest requests, and one endless thread mixes topics, so
answers get worse while the student sees nothing. A real planner step reads ≈ 11 000 input tokens (panel v4), most of
it the fixed prefix (tools, system, base); the model window (≈ 200k) is never close. The owner wants several chats with
switching and a ring near the input that shows the context running out. Automatic summarization is not planned now.

## Decision

### A chat is a thread
- A chat **is** an `agent/thread` aggregate (no new aggregate, no new context). It gains `title`, `archived` and
  `lastActive`. The key stays the subject key; it is no longer unique: a key has many chats.
- **Placement (proposed, Open 1):** variant A — a chat lives inside a subject or «Без предмета», keeping the owner's
  2026-10-21 decision «история по предметам». The context chip picks the subject; the chat list shows the chats of that
  subject; picking a chat sets the chip. A request whose scope subject differs from the chat key is refused in the
  domain (`ErrInvalidThread`). Variant B (chats free of subjects, subject per request) is not taken: it breaks
  per-subject history and the Hub `thread_id` binding stays the same anyway.
- **Current chat** of a key: the unarchived chat with the newest `lastActive`; `Ask` without a chat id goes there and
  creates the first chat if none (today's behaviour). `Ask` with a chat id goes to that chat.
- **New chat**: «Новый чат» stores an empty thread of the current key and makes it current; if the current chat of the
  key is already empty, it is returned instead (no piles of empty chats). Title: «Новый чат» until the first request, then the student's first text cut to 40 runes on a word boundary.
  Titles come only from the student, never from the model (hard rule 3, ADR-0022).
- **Rename**: 1–80 runes, no control characters, checked in the aggregate.
- **Archive / restore**: archive, never delete (journal lines reference `thread_id`; history purge stays 90 days).
  Archive is allowed only when the current request is final (`DONE`, `FAILED`, `CANCELLED`, `STOPPED`) or there is
  none, else `ErrInvalidTransition` (UI offers «Стоп» first). An archived chat is read-only until restored.
- **Switch**: allowed at any time; a running request of another chat keeps running and its updates carry its
  `threadId`; the chat list marks it. Budget, plan, provenance (`seen`) and Undo stay per thread, so per chat.
- **Earlier requests on switch** (Open 3): shown read-only as the student's text and a desktop line built from the
  validated planner texts (as `thread.Reply`, Russian labels); live cards with buttons only for the current request.
- **Hub**: no change. `thread_id` is already opaque and per thread; the seal and kept turns are bound to it, so each
  chat is its own seal domain.
- **Migration** (next free desktop number): rebuild `agent_threads` without `UNIQUE (subject_key)`, add `title`,
  `archived_at`, `last_active_at`, index `(subject_key, archived_at, last_active_at)`. Each existing thread becomes the
  first chat of its key, title «Основной чат». Mirrored in `docs/contracts/schema.sql`.

### What «context used» means
- **Measure: the fill of the desktop history window**, not real tokens. `thread.Fill(entries, current)` (pure, next to
  `Window`, same pair grouping) returns `max(pairs/12, bytes/24 KiB, turns/64)` for the earlier requests of the chat
  that the next request would carry, and `Overflow` when some earlier pair is no longer sent.
- **Why**: it is exactly what decides what the agent still «remembers» of this chat, it grows with the conversation and
  only with it, it is free, deterministic, offline and known before sending. Real `usage.input_tokens` (already in
  `AgentStepResponse.usage`, the Hub sums input + cache write + cache read) against the model window would sit at
  5–15 % forever and mostly measure the fixed prefix and the tool results of the current request, so it never warns.
- **Where the number comes from**: the desktop domain; no contract change. If the owner prefers real tokens (Open 2,
  variant Б), the Hub adds `context_window_tokens` per profile to `AgentStepResponse` (additive field 6, `buf breaking`
  green) and the ring shows last-step input / window; that is a separate conditional task.

### The ring
- A small ring left of the send button, filled to `Fill` (capped at 100 %), `role="img"` with a hidden text «Контекст
  чата: N %»; tooltip with the same text.
- Thresholds (Open 4): < 70 % — neutral (secondary colour); 70–89 % — accent; ≥ 90 % — accent plus a hint line under the
  input «Чат становится длинным — начать новый чат» with the «Новый чат» action. Overflow: full ring and the hint
  «Ранние сообщения этого чата агент уже не видит — начать новый чат». One accent, no red (agent-core decision).
- Recomputed when a chat is opened and when a request of it ends (sent in the update), never during a step.

### Reserved: summarization (not built)
- Future slot: when a chat overflows, its earlier pairs may be replaced by one **summary unit** built by a Hub
  operation (e.g. `SUMMARIZE_CHAT`, planner alias, server prompt) and stored in the thread (`summary_upto` request
  number). `Window` would send it as the first history turn (a new additive `AgentTurn` kind, unsealed, ≤ 2 KiB, order
  rules on the Hub like `agent_reply`); `Fill` would count it as one unit. Summary text is untrusted model text: data
  only, never instructions or a cause of actions.
- Nothing of this is in the schema or contract now; the domain functions keep the unit list shape so the slot fits.

## Consequences
- `cases/agentturn`: `ThreadRepository.GetByKey` becomes «current chat of a key»; `AskInput` gets an optional thread
  id. New use case `cases/agentchats` (list, new, rename, archive, restore, fill, earlier requests) with its own ports.
- `input/wails.AgentService`: chat methods and `threadId` in `AgentAskInput`; `agent:update` carries the fill at the
  end of a request.
- The journal shows the chat title next to the subject.
- No change to the per-request budget (8 steps, 60k planner input) or to the Hub.

## Open (owner)
1. Chat inside a subject (variant A, proposed) or chats free of subjects (variant B)?
2. Ring measure: fill of the history window (proposed) or real last-step tokens against the model window (needs the Hub
   field)? Should the tooltip also show tokens?
3. On switching, are earlier requests as read-only text enough (cards with buttons only for the current request)?
4. Thresholds 70 % / 90 %, and the hint texts above?
5. Should the window limits (12 requests / 24 KiB) grow once chats exist, since a chat is a deliberate topic?
6. Is deleting a chat needed (beyond archive)? Its journal lines would stay.
