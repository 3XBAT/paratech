# ADR-0023: Hub: AgentStep and prompts on the server

- **Status:** Accepted
- **Date:** 2026-10-22
- **Source:** TASK-112; design `agent-core-design-2026-10.md` §1 (outside the repo); ADR-0005, ADR-0006, ADR-0010,
  ADR-0021, ADR-0022.
- **Amends:** ADR-0005(b) and ADR-0010 for `INSPECT_MATERIAL`, `ANSWER`, `TRIAGE_ASSIGNMENT`.

## Context
The agent planner needs tool use with a cheap model, and the owner wants prompts and keys only on the Hub. Today the
desktop sends `instructions` and a model alias with each `Complete`. A client that could send any alias or prompt
could turn the Hub into a general chat or pull the prompt out. A client could also forge the model's own past tool
calls in the history; the seal stops that. It does not stop free text in student turns, which is bounded by limits.

## Decision
- **New RPC** `GatewayService.AgentStep` (additive, `buf breaking` green). Request: idempotency key, `thread_id`,
  `profile` (closed enum, `PANEL`), `catalog_version`, base, scope, `turns` of this thread (student text | sealed
  calls | tool results), `max_output_tokens`. Response: sealed calls (1–4), usage, quota, reservation id, `replayed`.
- **Hub chooses** prompt, tool catalog and alias by profile (`planner` → Haiku-class). The client sends no alias and no
  prompt; an unknown catalog version is `invalid_argument`. `max_output_tokens` is clamped per profile and is the
  basis of the reservation.
- **New operation types**: `AGENT_STEP`, `INSPECT_MATERIAL` (planner alias), `ANSWER` (strong alias, Sonnet),
  `TRIAGE_ASSIGNMENT` (strong). The three readers go through `Complete` with the server prompt of their operation:
  non-empty client `instructions` or a client alias different from the server one is `invalid_argument`; the
  fingerprint uses the server prompt version. Hub migration `0006_agent_operations.sql` adds the ledger labels.
- **Seal** on every returned set of calls: HMAC-SHA256 over a canonical length-prefixed encoding of API key ID,
  `thread_id`, profile, `catalog_version` and each `(call_id, tool, arguments_json)` in order; prefixed by a key
  version byte for rotation; compared in constant time. The secret is ≥ 32 random bytes from the Hub secret config,
  never in Postgres, used only for seals. An echo with a wrong seal is refused.
- **Returned calls** are validated against the tool schema of the catalog version (UI-tool text fields ≤ 300
  characters); a call that fails is `PROVIDER_ERROR`.
- **Tool results** in `turns` are trusted only as size-limited data, never as instructions.
- **Ledger per step**: reserve → call → settle, one key per step, fingerprint over the whole request, domain-separated
  by RPC (`AgentStep` and `Complete` fingerprints never match); a repeat is a free replay. Key lifecycle as in
  ADR-0005(d).
- **Limits**: ≤ 4 calls per step; `student_text` ≤ 4 KB; `result_json` ≤ 16 KB; request ≤ 256 KB; ≤ 64 turns; tool
  name in the catalog of the version; `arguments_json` a JSON object ≤ 8 KB. Full argument check is on the desktop too.
- **Provider**: Anthropic tool use with `tool_choice: auto` (forced `any`/`tool` is a 400 on the 5.x models); the
  system prompt demands a tool call, and UI tools (cards) cover every outcome. A text-only answer or a stop reason
  other than `tool_use` is `PROVIDER_ERROR`, not retryable. No `thinking` field (disabling it is a 400 on 5.x).
  Prompt caching on the stable prefix (tools + system + base) and the last turn. Strict tool schemas are not sent
  (limits in `docs/refs/anthropic.md` section 7); the Hub checks every call itself.
- **Charging unusable answers**: a billed answer that is unusable (text only, output cap, invalid calls) is settled
  with its usage and stored as a failure marker, so a retry with the same key replays the same failure without a new
  call. Refusals and errors without usage are released.
- **Output floor**: `max_output_tokens` below the profile floor (default 256) is raised to it before the alias cap,
  so a tiny cap cannot buy a free call; the reservation uses the raised cap.
  TASK-172: floor 2048 and planner cap 4096 (a beta step cut at 1024); the log line has the stop reason.
- **No streaming**: a planner step is short (~400 output tokens, up to the 4096 cap, TASK-172); the desktop shows progress between steps.
  `CompleteStream` stays unimplemented.
- **Prompts in `hub/pkg/prompts`** (`go:embed`, ARCH-04): `agent/<profile>/v<N>/` (`system.md`, `tools.json`) and
  `ops/<op>/v<N>/`. The version is in the path; `app` loads and validates them at start and hands them to the use case
  as data. The client sends only the version.
- **Logs**: the Hub writes no texts; results kept ≤ 24 h as in ADR-0005(d).

## Consequences
- Use case `hub/cases/agentstep`: rate limit → limits → seal check → reserve → kept turn → provider → schema check →
  keep turn → settle → new seal.
- Cache reads/writes are counted as ordinary tokens in the tester's quota; the saving is the owner's.
- `prompt_ref` for existing operations (`SUMMARY`, `EXTRACT`, `DRAFT`, `LECTURE_TEXT`) comes later, one operation at a
  time; it breaks the replay fixtures of TASK-093 until they are updated.
- The seal is bound to the thread but not to the turn/step position, so a sealed call set could be echoed again within
  the same thread; the desktop provenance and plan checks bound that.
- Open: minimum cacheable prefix length and prices are to be checked in `docs/refs/anthropic.md` before TASK-114.
- Thinking (TASK-116): with default thinking the API wants the last assistant turn back with its `thinking` blocks
  and signatures unchanged. The Hub keeps the raw content of every answer whose calls it returns in Postgres
  `agent_turns` (migration 0007), keyed by API key ID + `CallSetKey` (SHA-256 of thread, profile, catalog version and
  calls, what the seal binds), written before settle, purged with stored results (≤ 24 h), never logged or returned.
  The next step sends it in place of the last echo (earlier echoes stay plain `tool_use`). No kept turn:
  `failed_precondition` before the provider call, nothing charged; a replay does not need it. A failed write is only
  logged (the answer stands, its continuation fails). The kept turn is not in the reservation; usage is settled as is.

## Addendum: History pairs (TASK-181, 2026-10-26)
Earlier requests went as bare student texts, merged into one user message, so the model answered all of them
(TASK-140). Owner's decision (variant «а»): the model sees pairs «student request → app's answer» and answers only
the last request.
- **Turn kind** `agent_reply` (`AgentTurn` oneof field 4, additive): what the app showed the student in answer to an
  earlier request of the thread, built by the desktop (TASK-183). Non-blank UTF-8 without NUL, ≤ 2 KiB.
- **Order**: directly after a student text and directly before a student text (never first, last, next to an echo or
  results, or two in a row). Counted in the 64-turn and 256 KiB limits; the fingerprint encodes the turn kind.
- **Wire**: an assistant message with one text block, so pairs alternate user/assistant; the current request's
  echo and kept thinking turn and the cache point on the last block are unchanged.
- **No seal**: like a student text, it is bounded client data and chooses no action (plan and provenance checks stay
  on the desktop).
- **Old desktops** (catalogs 1–3, no `agent_reply`): when a request has no agent reply at all, the Hub puts the fixed
  assistant text "(earlier request; its answer is not available)" between consecutive student texts instead of
  merging them; it counts in the reservation.
- **Deployment**: the Hub with this change goes out before a desktop that sends `agent_reply`; an older Hub answers
  `invalid_argument` to the unknown turn kind.

## Addendum: Long replies (TASK-223, 2026-10-27)
Owner's decision: a big question gets a long answer. Amends "UI-tool text fields ≤ 300 characters" for `reply.text`.
- **Catalog 7**: `reply.text` has the type `reply_text`, ≤ 4000 characters, plain text (no HTML); only `reply.text`
  may have it, from catalog 7. Every other card text stays ≤ 300; catalogs 1–6 are unchanged.
- **Over the limit** (catalog 7 only): a text-only answer (TASK-200) and a returned `reply` call are cut at a sentence
  with " …" before the schema check, also to keep `arguments_json` ≤ 8 KiB; the cut calls are sealed, stored and
  echoed. `<` and `>` are allowed in a text-only answer, shown as text. The adapter takes returned arguments up to
  32 KiB, so a `reply` over 8 KiB from the provider is cut too; arguments over 32 KiB, and any other call over 8 KiB,
  stay the charged `PROVIDER_ERROR` (as do all of them on catalogs 1–6).
- **History**: `agent_reply` stays ≤ 2 KiB; the desktop already cuts a long answer for it.
- **Deployment**: the Hub with catalog 7 goes out before the desktop that requests it.
