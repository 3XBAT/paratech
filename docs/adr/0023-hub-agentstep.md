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
- **Provider**: Anthropic tool use with `tool_choice: any`, so every answer is a tool call (cards are UI tools). A stop
  reason other than `tool_use` is `PROVIDER_ERROR`, not retryable. Prompt caching on the stable prefix (tools + system
  + base) and the last turn; strict tool schemas only if confirmed in `docs/refs/anthropic.md`.
- **No streaming**: a planner step is short (~400 output tokens); the desktop shows progress between steps.
  `CompleteStream` stays unimplemented.
- **Prompts in `hub/pkg/prompts`** (`go:embed`, ARCH-04): `agent/<profile>/v<N>/` (`system.md`, `tools.json`) and
  `ops/<op>/v<N>/`. The version is in the path; `app` loads and validates them at start and hands them to the use case
  as data. The client sends only the version.
- **Logs**: the Hub writes no texts; results kept ≤ 24 h as in ADR-0005(d).

## Consequences
- Use case `hub/cases/agentstep`: rate limit → limits → seal check → reserve → provider → schema check → settle → new
  seal.
- Cache reads/writes are counted as ordinary tokens in the tester's quota; the saving is the owner's.
- `prompt_ref` for existing operations (`SUMMARY`, `EXTRACT`, `DRAFT`, `LECTURE_TEXT`) comes later, one operation at a
  time; it breaks the replay fixtures of TASK-093 until they are updated.
- The seal is bound to the thread but not to the turn/step position, so a sealed call set could be echoed again within
  the same thread; the desktop provenance and plan checks bound that.
- Open: minimum cacheable prefix length and prices are to be checked in `docs/refs/anthropic.md` before TASK-114.
