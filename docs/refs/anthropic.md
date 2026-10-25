# Anthropic Messages API — reference for TASK-034 (non-streaming) and TASK-144 (streaming, section 8)

Retrieved: 2026-10-15

Sources actually fetched (official docs only; `docs.claude.com/en/api/*` 301-redirects to `platform.claude.com/docs/...`):
- https://platform.claude.com/docs/en/api/overview
- https://platform.claude.com/docs/en/api/versioning
- https://platform.claude.com/docs/en/api/messages/create (request params + Returns section)
- https://platform.claude.com/docs/en/api/messages
- https://platform.claude.com/docs/en/api/errors
- https://platform.claude.com/docs/en/api/rate-limits
- https://platform.claude.com/docs/en/build-with-claude/handling-stop-reasons
- https://platform.claude.com/docs/en/about-claude/models/overview (served as /docs/en/models/overview)
- https://platform.claude.com/docs/en/about-claude/model-deprecations
- https://platform.claude.com/docs/en/about-claude/pricing

**anthropic-version: `2023-06-01`** (only versions listed: `2023-06-01`, `2023-01-01`; older ones are deprecated).

## 1. Endpoint and headers

`POST https://api.anthropic.com/v1/messages`

| Request header | Value | Required |
|---|---|---|
| `x-api-key` | API key ("legacy fallback for `Authorization`, still supported") | either this or `Authorization: Bearer <key>` |
| `anthropic-version` | `2023-06-01` | yes |
| `content-type` | `application/json` | yes |
| `anthropic-workspace-id` | `wrkspc_...` | only with a multi-workspace key |

| Response header | Meaning |
|---|---|
| `request-id` | unique id, e.g. `req_018E...`; same value as `request_id` in error bodies |
| `retry-after` | seconds to wait before retrying; not every 429 has it, the spend-cap 429 never does |
| `anthropic-ratelimit-{requests,tokens,input-tokens,output-tokens}-{limit,remaining,reset}` | limits; `reset` is RFC 3339; token `remaining` rounded to nearest thousand |
| `anthropic-organization-id`, `anthropic-workspace-id` | ids the key resolved to |

## 2. Request body (fields we need)

| Field | Notes from docs |
|---|---|
| `model` | string, required (ids in section 5) |
| `max_tokens` | number, required, minimum 0 (`0` = only populate the prompt cache) |
| `messages` | required; items `{role: "user"\|"assistant", content: string \| array of blocks}`; text block = `{"type":"text","text":"..."}`; no `system` role in messages; consecutive same-role turns are merged; max 100,000 messages per request; a final `assistant` message is prefill, which Claude 4.6+ models reject with 400 |
| `system` | optional, string or array of text blocks |
| `stop_sequences` | optional array of string; on match `stop_reason = "stop_sequence"` and `stop_sequence` holds the matched value |
| `temperature` | optional, 0.0–1.0, default 1.0. **Deprecated**: models released after Claude Opus 4.6 accept only `1.0`; any other value gives 400 (same for `top_p` <0.99 and any `top_k`). So: omit it. |
| `metadata.user_id` | optional, string, maxLength 512; opaque id (uuid/hash), no names/emails/phones |
| `stream` | optional, default false |

Max request size: **32 MB** for Messages (413 `request_too_large`, returned by Cloudflare before reaching the API).

## 3. Response body (200)

Fields: `id` (format may change), `type: "message"`, `role: "assistant"`, `model`, `content` (array of blocks), `stop_reason`, `stop_sequence` (string or null), `stop_details` (null except for refusals: `{type:"refusal", category, explanation}`), `usage`; also `container` and `diagnostics` (null for us).

Content block types: `text` (`text`, optional `citations`); others that may appear: `thinking`, `redacted_thinking`, `tool_use`, `server_tool_use`, `web_search_tool_result`, `web_fetch_tool_result`, `code_execution_tool_result`, `bash_code_execution_tool_result`, `text_editor_code_execution_tool_result`, `tool_search_tool_result`, `container_upload`. Current models think adaptively by default, so expect `thinking` blocks before `text`; concatenate only `type == "text"` blocks.

`stop_reason` (non-null in non-streaming mode): `end_turn`, `max_tokens` (requested limit or model max), `stop_sequence`, `tool_use`, `pause_turn` (long-running turn paused; resend to continue), `refusal` (classifiers intervened), `model_context_window_exceeded`. Docs warn enum values may be added.

`usage`:

| Field | Meaning / billing |
|---|---|
| `input_tokens` | input after the last cache breakpoint; billed at base input price |
| `output_tokens` | billed at output price; "inclusive, authoritative total used for billing" (includes thinking) |
| `cache_creation_input_tokens` (nullable) | billed at 1.25x (5 min) or 2x (1 h) base input |
| `cache_read_input_tokens` (nullable) | billed at 0.1x base input (0.05x Opus 5.5 / Sonnet 5.5, 0.025x Fable 5.1) |
| `cache_creation.{ephemeral_5m_input_tokens, ephemeral_1h_input_tokens}` | breakdown of cache writes by TTL |
| `output_tokens_details.thinking_tokens` | observability only, already inside `output_tokens` |
| `server_tool_use.{web_search_requests, web_fetch_requests}` | server tool counts (billed separately) |
| `service_tier` | `standard` / `priority` / `batch` |
| `inference_geo` | region; `inference_geo: "us"` request costs 1.1x |

Total input = `input_tokens + cache_creation_input_tokens + cache_read_input_tokens`. `output_tokens` is non-zero even for an empty reply.

## 4. Errors

Shape (always JSON):
```json
{"type":"error","error":{"type":"not_found_error","message":"..."},"request_id":"req_..."}
```
Docs: `type` values "may expand". Spend-cap 429 also has `error.details.error_code = "enforced_spend_limit_reached"`.

| HTTP | `error.type` | Notes from docs |
|---|---|---|
| 400 | `invalid_request_error` | bad request; also used for other unlisted 4xx; also when a user-set org/workspace spend limit is reached |
| 401 | `authentication_error` | key malformed, revoked or expired |
| 402 | `billing_error` | billing/payment problem |
| 403 | `permission_error` | key lacks permission |
| 404 | `not_found_error` | |
| 409 | `conflict_error` | "Resolve the conflict, then retry" |
| 413 | `request_too_large` | > 32 MB |
| 429 | `rate_limit_error` | rate/acceleration limit (wait `retry-after`), or tier spend cap (no `retry-after`, keeps failing until next month) |
| 500 | `api_error` | "Retry the request with exponential backoff" |
| 504 | `timeout_error` | timed out while processing; docs suggest streaming for long requests |
| 529 | `overloaded_error` | API temporarily overloaded |

Retryable per docs: the official SDK retries "connection errors, rate limits, and 5xx server errors" with exponential backoff, 2 retries by default, honoring `retry-after`. Explicit doc advice: 500 retry with backoff; 429 wait `retry-after`; spend-cap 429 is not retryable.

Long requests: docs recommend streaming or Message Batches for long requests, "especially those over 10 minutes"; avoid large `max_tokens` without streaming because networks may drop idle connections; TCP keep-alive helps. SDKs refuse non-streaming requests expected to exceed a 10-minute timeout (their curl example uses `--max-time 600`).

## 5. Models (Claude API ids, copied verbatim)

Current lineup (models overview): all 1M-token context window, 128K max output (synchronous Messages API).

| Model | API id = alias | Price in/out per MTok | Retirement not sooner than |
|---|---|---|---|
| Claude Fable 5.1 | `claude-fable-5-1` | $10 / $50 | 2027-09-01 |
| Claude Opus 5.5 | `claude-opus-5-5` | $4 / $20 | 2027-09-22 |
| Claude Sonnet 5.5 | `claude-sonnet-5-5` | $2 / $10 | 2027-09-28 |
| Claude Haiku 5.5 | `claude-haiku-5-5` | $0.10 / $0.50 (≤100K prompt; $0.50 / $2.50 above) | 2027-10-07 |

Docs: "Every Claude model ID is a pinned snapshot, including the dateless IDs used from the 4.6 generation on."

Legacy, still available ("Active" in deprecations table): `claude-fable-5`, `claude-mythos-5-1`, `claude-mythos-5`, `claude-opus-5`, `claude-opus-4-8`, `claude-opus-4-7`, `claude-opus-4-6`, `claude-opus-4-5-20251101` (alias `claude-opus-4-5`), `claude-sonnet-5`, `claude-sonnet-4-6`, `claude-haiku-4-5-20251001` (alias `claude-haiku-4-5`; retirement not sooner than 2026-10-15). Context window / max output for legacy models: not verified (not on the overview page).

Deprecated: `claude-sonnet-4-5-20250929` (retires 2026-11-30), `claude-mythos-preview`. Retired (requests fail): `claude-opus-4-1-20250805`, `claude-opus-4-20250514`, `claude-sonnet-4-20250514`, `claude-3-7-sonnet-20250219`, `claude-3-5-haiku-20241022`, `claude-3-haiku-20240307` and older.

## 6. Redirects

Not stated for the API. (Only the docs site redirects: `docs.claude.com` -> `platform.claude.com`, 301.)

## Notes for the adapter

- Send `x-api-key`, `anthropic-version: 2023-06-01`, `content-type: application/json`. Do not send `temperature`/`top_p`/`top_k` (400 on current models unless default).
- Parse errors by HTTP status first, then `error.type`; keep `request-id` (header) for logs. Treat unknown `error.type` / `stop_reason` / block types as possible (docs say enums grow).
- Retry policy (our choice, from doc facts): 429 is transient unless `error.details.error_code` = `enforced_spend_limit_reached`; `retry-after`, when present, says how long to wait. Also retry 500, 504, 529 and connection errors with backoff; do not retry 400/401/402/403/404/413 or a 429 with `enforced_spend_limit_reached`.
- Billing: settle quota from `input_tokens + cache_creation_input_tokens + cache_read_input_tokens` and `output_tokens` (nullable cache fields -> 0).
- Text result = join of `text` blocks; `stop_reason == "max_tokens"` means truncated output; `refusal` means no usable result.
- Non-streaming calls over ~10 minutes are discouraged; set an explicit client timeout (<= 10 min) and TCP keep-alive (Go's default `net.Dialer` already enables keep-alive — our inference).
- **Our inference, not a doc fact:** Go's `http.Client` strips only `Authorization`/`Cookie`-like sensitive headers on cross-domain redirects; a custom `x-api-key` header is forwarded. Since the docs never mention API redirects, the client should refuse redirects (`CheckRedirect` returning `http.ErrUseLastResponse` or an error) so the key cannot leak.
- Cap response body read size (our inference; no response size limit is documented, max output is 128K tokens).
- Used by `hub/internal/adapters/anthropic` (TASK-034): instructions go to `system`, untrusted data to the one user text block; 8 MiB response cap; redirects refused; a 200 answer cut off mid-body is the transient "answer interrupted" (probably billed).

## 7. Tool use and prompt caching (used by TASK-114, `Step`)

Not re-fetched for TASK-114 (no network in that session); the fields below are the long-standing Messages API ones. Items marked **to verify** must be checked against the live docs before the agent ships.

Request (non-streaming, same endpoint and headers):
- `tools[]`: `{name, description, input_schema}`; `input_schema` is a JSON Schema object (`type: object`, `properties`, `required`, `additionalProperties`). The Hub builds it from the catalog (`domain/llm/services/toolcatalog`, every object has `additionalProperties: false` and lists `required`); `maxLength`, `pattern`, `enum` are hints to the model, the Hub re-checks every returned call itself.
- `strict: true` on a tool (structured outputs) is **not sent**. Restrictions as remembered from the structured-outputs docs, **to verify** against the live page: every object needs `additionalProperties: false`; `minLength`/`maxLength`, `minimum`/`maximum`, `maxItems`, `minItems` above 1 and a schema-valued `additionalProperties` are unsupported (400); at most 20 strict tools and 24 optional parameters per request. The catalog has 27 tools and `plan_steps.arguments` is an open map, so strict on every tool would fail every call. Revisit with a strict-compatible catalog version.
- `tool_choice: {"type": "auto"}`. Forced tool choice (`{"type": "any"}` / `{"type": "tool"}`) returns 400 "tool_choice: type tool and any are not supported for this model" on Claude Opus 5.5, Sonnet 5.5 and Fable 5.1 (per the API reference, reported by the supervisor of TASK-114; not re-fetched here). With `auto` the model may answer in plain text: the system prompt demands a tool call, and a text-only answer is a charged `PROVIDER_ERROR` (ADR-0023).
- `thinking`: not sent. On Opus 5.5 / Sonnet 5.5 thinking cannot be disabled (`{"type": "disabled"}` returns 400), so the model default (adaptive) applies.
- Thinking in a tool loop: the last assistant turn must go back with its `thinking` / `redacted_thinking` blocks first and unchanged (signature included), or the API answers 400 ("a final `assistant` message must start with a thinking block"); thinking blocks of earlier turns may be omitted (as remembered from the extended-thinking docs; the omission and the exact error text are **to verify** live). The Hub keeps the raw `content` array of each answer and sends it as that turn (ADR-0023, TASK-116); `testenv.FakeMessagesAPI` enforces the rule in tests.
- Assistant turn with echoed calls: the kept raw content for the last echo; earlier echoes as content blocks `{type: "tool_use", id, name, input}` (`input` an object).
- User turn with results: blocks `{type: "tool_result", tool_use_id, content, is_error}` (`content` a string, here the result JSON; `is_error` only when true). Every `tool_use` must be answered by a `tool_result` in the next user message; the results come first in that message.
- `cache_control: {"type": "ephemeral"}` on a block marks the end of a cached prefix (order: tools, system, messages). The Hub sets two breakpoints: the student-context system block (tools + system prompt + base) and the last block of the history.

Response: `stop_reason: "tool_use"` when the model called tools; `content` holds `tool_use` blocks (`id`, `name`, `input`), possibly after `thinking`/`text` blocks, which the Hub does not read but keeps raw for the next step. The stop reason is read first: `refusal` is released, not charged. Any other stop reason, or no `tool_use` block, is `PROVIDER_ERROR` (permanent, charged with its usage, ADR-0023).

Usage: unchanged; cache writes and reads are in `cache_creation_input_tokens` / `cache_read_input_tokens` and are settled as input tokens (section 3).

**To verify** before rollout:
- minimum cacheable prompt length per model (below it `cache_control` is ignored, no error) and whether the planner prefix (tools + system + base, roughly 4-6K tokens) reaches it for Haiku 5.5;
- current cache write/read prices for Haiku 5.5 and Sonnet 5.5 (section 3 lists multipliers from 2026-10-15);
- the strict-mode restrictions listed above;
- the exact charset/length of `tool_use.id` (the Hub accepts 1-128 of `[A-Za-z0-9_-]`).

## 8. Streaming (used by TASK-144)

Retrieved: 2026-10-24 from https://platform.claude.com/docs/en/build-with-claude/streaming (docs.anthropic.com 301-redirects there). Facts below are from that page; anything marked **our inference** is not a doc fact. Not verified against the owner's gateway (ai.starimg.ru) — the TASK-144 probe is that check.

Request: same endpoint and headers, body field `"stream": true`. The response is server-sent events (SSE).

SSE framing: each event is an `event: <name>` line followed by a `data: <json>` line; events are separated by a blank line. The `data` JSON carries a `type` equal to the event name. Example verbatim:
```
event: content_block_delta
data: {"type": "content_block_delta","index": 0,"delta": {"type": "text_delta", "text": "ello frien"}}
```

Event flow (docs, in order):
1. `message_start` — a `Message` object with empty `content`. Example: `{"type": "message_start", "message": {"id": "msg_...", "type": "message", "role": "assistant", "content": [], "model": "claude-opus-5-5", "stop_reason": null, "stop_sequence": null, "usage": {"input_tokens": 25, "output_tokens": 1}}}`. In the thinking example `usage` is absent from `message_start`, so it must be treated as optional.
2. Content blocks, each: `content_block_start` (`{"type":"content_block_start","index":N,"content_block":{...}}` — e.g. `{"type":"text","text":""}`, `{"type":"tool_use","id":"toolu_...","name":"get_weather","input":{}}`, `{"type":"thinking","thinking":"","signature":""}`), one or more `content_block_delta`, then `content_block_stop` (`{"type":"content_block_stop","index":N}`). `index` = position in the final `content` array. Exception: during server-side fallback a `fallback` block arrives as a start/stop pair with no deltas.
3. One or more `message_delta` — top-level changes: `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":89}}`. **The token counts in `message_delta.usage` are cumulative** (docs warning). In the thinking example `usage` is absent here too.
4. `message_stop` — `{"type":"message_stop"}`.

`ping` events (`{"type": "ping"}`) may appear any number of times anywhere.

Error events can arrive mid-stream (HTTP status is already 200): 
```
event: error
data: {"type": "error", "error": {"type": "overloaded_error", "message": "Overloaded"}}
```
`overloaded_error` "would normally correspond to an HTTP 529 in a non-streaming context". `error.type` values are those of section 4.

Unknown event types: "new event types may be added, and your code should handle unknown event types gracefully."

Delta types (`content_block_delta.delta.type`):
- `text_delta` — `{"type": "text_delta", "text": "..."}`; append to the `text` block.
- `input_json_delta` — `{"type": "input_json_delta", "partial_json": "{\"location\":"}`; partial JSON strings for `tool_use.input`; the first delta may be `""`. Accumulate the strings and parse once at `content_block_stop`; the final `input` is always an object. Current models emit one complete key/value at a time, so there may be pauses between events.
- `thinking_delta` — `{"type": "thinking_delta", "thinking": "..."}`; append to the `thinking` field.
- `signature_delta` — `{"type": "signature_delta", "signature": "..."}`; sent just before `content_block_stop` of a thinking block; it verifies the block's integrity (so it must be kept with the block for the next turn, section 7). With `display: "omitted"` a thinking block gets one `thinking_delta` with an empty string, then one `signature_delta`.

stop_reason: `null` in `message_start`, delivered in `message_delta.delta.stop_reason` (values as in section 3).

Interrupted streams (docs "Error recovery"): for Claude 4.6+ the documented resume is a NEW request with a user message containing the partial text and an instruction to continue; tool_use and thinking blocks "cannot be partially recovered". **Our inference:** the Hub does not resume; an interrupted stream is a failed attempt like a body cut off mid-answer in the unary adapter (section 6 notes): the reservation is released, nothing is charged, and the call is never retried once a delta went out (ADR-0026, ADR-0010).

**Our inference** for the adapter: read line by line, ignore `ping` and unknown event names, and keep only what the gateway uses: the text of `text_delta`s (also the text of a `content_block_start` text block), the stop reason and the usage (`message_start.usage` input fields + the last cumulative `message_delta.usage` values). It does not rebuild the full Message (thinking, signatures and tool blocks are dropped; `Stream` serves only the ANSWER reader). Apply a response-size cap, and map an `error` event by `error.type` like the HTTP statuses of section 4.
