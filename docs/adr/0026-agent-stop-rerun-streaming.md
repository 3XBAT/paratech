# ADR-0026: Agent panel: waiting, «Стоп», repeat and streamed answers

- **Status:** Proposed
- **Date:** 2026-10-24
- **Source:** owner's beta feedback of 2026-10-24 (agent panel); ADR-0010, ADR-0021, ADR-0022, ADR-0023;
  `paratech-ops/supervisor-decisions.md` (Interrupt keeps real outcomes).
- **Amends:** ADR-0023 «No streaming» (only for `CompleteStream` of `ANSWER`; `AgentStep` stays unary) and the
  cancel rules of `CompleteStream` in `gateway.proto` (comment only, wire unchanged).

## Context
The panel shows only «Отправка…» while a request runs. The core already has `Stop`, `Continue`, `Retry` (failed step,
new key, ≤ `thread.MaxRetries`), `Restart` (expired) and `Cancel`, bound in `input/wails` and shown on stop cards. Gaps:
the panel learns the thread and turn of a new request only when the first Hub step returns, so «Стоп» cannot be
pressed during it; after «Стоп» the Driver still runs the remaining calls of the step; there is no way to run a
finished request again. The long text the student waits for is the `ANSWER` reader (Sonnet, up to 8 claims of 400
characters); planner steps are short tool calls and `reply` is ≤ 300 characters. `CompleteStream` exists in the
contract but is not implemented. Streaming through the owner's gateway (`ai.starimg.ru`) is unverified.

## Decision

### Waiting
- While a request is `PLANNING` or `EXECUTING` without a job progress card, the panel shows three softly pulsing dots
  in the accent colour (the `breath` animation of the mockup), no word; `role="status"` with a hidden text «Запрос
  выполняется»; static dots under `prefers-reduced-motion`. Job progress cards stay as they are.
- `agentturn` gets an optional port `Progress { Started(ctx, Step) }`, called after the open step is saved and before
  the Hub call; it must not block and never calls back into the service. `input/agent` provides the implementation
  over its `Sink` (no Driver ↔ Service cycle in `app`). It publishes `Update{…, Progress: true}` without cards; the
  panel applies a progress update to status only and keeps the cards it shows.

### «Стоп» (interrupt a running request)
- `Stop` keeps its rule: the request is stored `STOPPED/user` at once, without waiting for the running step.
- **Planner step in flight**: the Hub call is not cancelled. It is paid once; its reply is dropped by the version
  conflict; «Продолжить» sends the same key and gets a free replay (≤ 24 h, then «Начать заново»).
- **Calls of the step**: after `Stop` the Driver runs no further call of that step. A read (class R) in flight is cut
  through its own context and answered not done; a write (class U) in flight finishes and reports its real outcome
  (journal `DONE`/`FAILED`, action line with «Отменить»). Then `Interrupt` closes the step; a request stopped by the
  student keeps reason `user`.
- **Paid Hub reads** (`answer_question`, `inspect_upload`, triage): the Hub finishes a call whose client went away,
  settles it in full and stores it (`Complete` already does; `CompleteStream` the same, below). The answer key is
  derived from the data, so «Продолжить» replays it for free.
- **Paid jobs** started by the request keep running; «Стоп» never stops them. Its action line «Отменить» stops a
  running job (ADR-0021). **Proposals** (class C) stay open cards; nothing is sent without the student's click.

### «Повторить» (run a finished request again)
- `Rerun(ref)` in `agentturn`, `Driver`, `AgentService`: allowed on the current request of the thread when it is
  `DONE`, `FAILED`, `CANCELLED` or `STOPPED` (any reason); else `ErrInvalidTransition`. It cancels a stopped request,
  closes its open calls as not done, and starts a new request (next turn) with the stored student text and scope.
- **Not over live effects**: refused (`ErrRerunBlocked`, problem `rerun_blocked`) when the request has a journal entry
  that is not `FAILED` or `UNDONE` or an open proposal. The fact comes from port `agentturn.Effects` (implemented by
  `cases/agentjournal`, wired by `app`); the rule lives in `thread.Rerun`. So a repeat never doubles a write or a
  proposal; the student can «Отменить всё» first.
- **Idempotency**: the new request has new step keys `ag:<thread>:<turn+1>:<step>` and is paid again. A second click
  names the old turn and is `stale`. The answer reader key gets the request as a scope (`ans:v2:`, digest of data,
  places and `ag:<thread>:<turn>`), so a repeat gives a fresh answer while «Продолжить» in the same request still
  replays.
- Labels: the stop card of a failed step keeps «Повторить» = `Retry` (same request, new key). The last finished request
  shows «Повторить» = `Rerun`. Both never appear together. «Начать заново» stays for expired requests.

### Streaming
- **What streams**: only the `ANSWER` reader output. Planner steps (`AgentStep`) stay unary; a tool-call step shows the
  waiting dots; `reply` appears whole.
- **Hub**: `CompleteStream` is implemented for `OPERATION_TYPE_ANSWER` only (other types `invalid_argument`), with the
  validation, server prompt, fingerprint, reserve → provider → settle → store of `Complete`. Same key and spec in
  `Complete` and `CompleteStream` is one logical request, so either replays the other's result. The provider call runs
  on a context detached from the client (bounded by the call timeout); if the client goes away the Hub reads the
  provider to the end, settles once in full and stores the result. No partial settlement is used. A provider stream
  that ends without `message_stop` is `PROVIDER_ERROR`, released. The Hub schema check of `ANSWER` runs on the whole
  text; a replay sends the stored output as one delta, then the result.
- **Port** in `hub/internal/cases/gateway/ports.go`:
  `type StreamingProvider interface { Stream(ctx context.Context, req values.Request, onDelta func(chunk string)) (values.Completion, error) }`
  with the error rules of `Provider.Complete`; `onDelta` is called in order from the calling goroutine and the
  concatenation equals the completion output.
- **Flag**: Hub config `provider_stream` (default false). Off: `CompleteStream` calls `Provider.Complete` and sends the
  whole output as one delta. On: the anthropic adapter uses `stream: true` (SSE). The owner turns it on after the probe
  against the gateway passes; the desktop code is the same either way.
- **Desktop**: `hubclient.AnswerQuestion` uses `CompleteStream` and hands each accumulated output to a callback;
  `Unimplemented` falls back to `Complete` with the same key. `answer.Draft(partial)` (pure, in
  `domain/coursework/answer`) extracts the claim texts written so far from partial JSON (never fails, bounded, control
  characters dropped). `answering` passes drafts to an optional `DraftSink` of the ask; `input/agent` binds it to
  (thread, turn, call) and `Sink.Draft`; `input/wails` emits `agent:draft {threadId, turn, callId, texts}` at most
  10 times a second, last text wins.
- **Trust**: draft text is untrusted model text, rendered as a text node only, marked «черновик», never stored, never
  sent to the planner and never a cause of any action. The verified Answer card replaces it; strict parsing and
  citation checks still run on the complete output. On a failure or «Стоп» the draft is removed.
- Prompts and the tool catalog do not change.

## Consequences
- Contract: `gateway.proto` comments change (cancel = finish and settle in full; only `ANSWER`); `buf breaking` stays
  green. `SettleRequest.partial` stays unused. Tests that pin `CompleteStream` as unimplemented are updated.
- `docs/refs/anthropic.md` gets a streaming section (event types, usage, errors) before the adapter is written.
- A rerun costs planner steps and a new answer; the Hub quota bounds it.

## Decided by the owner (2026-10-26; was «Open»)
1. «Повторить» gives a **fresh, paid** answer (a student who repeats wants something new), not the stored one.
2. A request whose effects cannot be undone is never repeated by the button; the student asks again by typing.
3. No limit on repeats beyond the quota.
4. Unverified streamed claim texts are shown as «черновик» before the citation check (TASK-146..148).
5. Streaming goes to beta testers (`provider_stream` on) after the owner runs the gateway probe and it passes.
   Superseded the same day by the addendum below: on now, without the probe.

## Addendum 2026-10-26 (TASK-190, owner's decision)
- `provider_stream` is **on by default**, without waiting for the gateway probe (supersedes item 5 above).
  `HUB_PROVIDER_STREAM=false` or `-provider-stream=false` turns it off.
- **JSON fallback**: if the provider (or a buffering gateway) answers the streaming request with 200 and
  `application/json`, the adapter reads it as `Complete` does and sends the whole output as one delta, with the usage
  of that call. No second paid call. The first such answer logs «provider answered without SSE» once per process, no
  texts. Any other non-SSE answer stays a provider error. The desktop fallback to `Complete` on `Unimplemented` stays.
- **Deltas vs result** (TASK-145): deltas are untrusted draft text. The joined deltas may differ from
  `CompletionResult.output` (for example a code fence the check strips); the checked result is what is stored, settled
  and replayed.

## Addendum: streamed reply (TASK-340, owner's beta feedback 2026-10-28)
Long replies (catalog 7+, up to 4000 characters) appeared whole at the end of a step. Supersedes «Planner steps
(`AgentStep`) stay unimplemented / `reply` appears whole» above for clients that use the new RPC.
- **Contract**: additive server-streaming `AgentStepStream(AgentStepStreamRequest{AgentStepRequest request})` returns
  `stream AgentStepStreamResponse{oneof event: string delta | AgentStepResponse result}` (a wrapper, as buf lint
  forbids one request type for two RPCs). Same validation, fingerprint, key and ledger as `AgentStep`: one logical
  request, either replays the other's stored result. Deltas, then exactly one `result`; a failure is a Connect error
  with the ErrorDetail of `AgentStep`, no `result`; a stream without `result` is failed, the client drops the draft (offline, see Desktop).
- **What streams**: only the top-level `text` argument of `reply` calls (read incrementally from `input_json_delta`:
  escapes, `\uXXXX` with surrogate pairs and UTF-8 split across chunks); a later `reply` call of the same step starts
  with a blank line. Text blocks never stream: with `tool_choice: auto` they are often a preamble before tool calls
  («Сейчас посмотрю расписание.») that would show as the reply and vanish; a text-only answer (TASK-200) appears whole
  in the result. Other tools' arguments and thinking never stream. Control characters except line breaks and tabs
  are dropped; at most 4000 characters in all; chunks are joined until 64 bytes are pending or 50 ms have passed (the
  first goes at once), and the handler cuts them only between characters (PR #331 rule). The 50 ms are checked only
  when the next chunk arrives (no timer), so after the model's last chunk up to 63 bytes may wait until the step ends.
- **Trust** (hard rule 3): deltas are display-only draft text, never stored, never sent to the planner, never a cause
  of an action. The result is built exactly as `AgentStep` builds it (whole-answer parse, catalog check, cut at a
  sentence, seal, kept turn, settle); the joined deltas may differ from it (cut, trimmed) and the result wins.
- **No deltas** on a replay, with `provider_stream` off (unary `Step`) and on a provider JSON answer to the stream
  (fallback of TASK-190, one paid call): the stream holds only the result. The desktop renders the result either way.
- **Provider**: port `agentstep.StreamingProvider.StepStream(ctx, call, onDelta)`; the anthropic adapter sends
  `"stream": true` and rebuilds the content array from the events (thinking with its signature, text, tool_use with
  its parsed input), so the kept turn of the thinking loop is the one a unary answer gives. Cancel, timeout, `message_stop`
  and settlement rules as for `CompleteStream` above. Prompts, catalog, caching, rate limit and reservation unchanged.
- **Desktop** (go-core): uses `AgentStepStream` for planner steps, falls back to `AgentStep` with the same key on
  `unimplemented` (an older Hub). A stored «Стоп» now cuts the step's stream on the client; the Hub still finishes,
  settles and stores the step, and «Продолжить» replays it free (refines «the Hub call is not cancelled» above). A
  stream that breaks without `result` ends the step as offline and offers «Продолжить» (same key, free replay), not
  «Повторить».
- **Stopped reply on screen**: after a student's «Стоп» the panel may keep the last shown reply draft, muted and
  labelled «Ответ остановлен». It is UI state only: never stored, never sent to the planner, removed by the next
  update of the thread. This refines «On a failure or «Стоп» the draft is removed» above for the reply draft only;
  answer drafts are still removed.
- To verify live: whether the planner model streams the `reply` argument in small pieces (the docs say current
  models may emit one whole key/value per `input_json_delta`); if not, a reply appears in one or few deltas.

## Addendum 2026-11-08 (owner's answers): retries, a cut reply, inactivity, heartbeat, the queue
Supersedes, where they differ: «Продолжить» after a lost connection (Waiting / Desktop above), decision 2 of
`progress/TASK-366.md` (24 h replay of a refused answer), «Decisions to confirm» of BUG-20261104 (core does not queue).

**1. Automatic retry (planner step and `answer_question`).**
- A Hub call that failed *transiently* (transport error, timeout, `unavailable`, Hub rate limit, a retryable
  `PROVIDER_ERROR`, a provider stream cut) is retried by itself after **5 s, 10 s, 20 s, at most 3 times**, under the
  **same key**: a released failure is not stored by the Hub, and a result the Hub did store is replayed free. Never
  retried by itself: quota, permanent failures, an unusable answer, an expired turn, the student's «Стоп».
- One schedule, pure: `domain/jobs/services/backoff.Interactive(attempt) (time.Duration, bool)` (agentturn already
  imports this package). The wait uses the `Clock` port, never holds a lock that `Ask`, `Stop` or another chat needs,
  and «Стоп» ends it at once. The panel gets a progress update with the attempt number (quiet line «Повторная попытка N
  из 3»).
- `answer_question`: only asks made for the student now (agent tool, a click) retry this way; job runs (tickets)
  keep the job queue's backoff.

**2. After the automatic retries.** A request whose failure came *from the Hub* (any `PROVIDER_ERROR`, a refusal, an
unusable answer) stops `failed`; only a request that never reached the Hub stops `offline`. «Повторить» on either is
`Retry`: a **new** key `…:r<N>` (`thread.Retry` is now allowed from `offline` too; budget `thread.MaxRetries`
unchanged). «Продолжить» with the same key stays only after the student's «Стоп» and after the quota. Losing a stored
result by the new key costs little: the three automatic attempts used the old key.

**3. `answer_question` keys.** The reuse scope becomes per call: `ag:<thread>:<turn>:<8 hex of sha256(call id)>`
(still `ans:v2:`, no migration; `MaxScopeBytes` holds it). A second `answer_question` of the model in the same request
(after a refusal) is a fresh paid attempt; «Продолжить» replays the same step with the same call ids, so it still
replays free.

**4. A cut reply is not «no connection» (E2E D4).** After the automatic retries the panel keeps the shown text with
«Ответ оборван. Можно повторить.» and the input stays enabled. «Нет связи с paratech» and a disabled input come only
from the connection state, never from one request's failure.

**5. Inactivity, not a total cap, for `answer_question`.** The streamed read gets the step's watchdog
(BUG-20261104): 60 s without a Hub message (2 min before the first one), 10 min overall; the 5-minute whole-read cap
goes.

**6. Hub heartbeat.** `CompleteStreamRequest` and `AgentStepStreamRequest` get `bool heartbeat` (additive, transport
only: **not** part of the fingerprint or key). When set, the Hub sends a new oneof event `StreamHeartbeat heartbeat`
(empty message) after 15 s without any message, until the result or the error, never after the result; not charged,
no data. One writer per stream in `hub/internal/input/connect`. The desktop sets the flag and resets its watchdog on
every message, so a 60 s cut means the Hub went silent, not the model. Old clients do not set it and see nothing new.
`HUB_LLM_TIMEOUT` is unchanged.

**7. A second question always waits (never refused).** While the chat's current request is `PLANNING` or
`EXECUTING` (a Hub step or a tool run alike), `Ask` returns at once with a *queued* ticket: an in-memory FIFO per chat
in `cases/agentturn` (at most 5; a sixth is the old busy refusal). The panel shows «ждёт предыдущий запрос» with the
icon «Отменить этот запрос» (`Withdraw`). When the current request becomes final (any final status, also after a
failure or the student's «Стоп» of that request) the oldest waiting question starts as the next request, with the
history including the first. A request waiting for the student (`AWAITING_PLAN`, `AWAITING_ANSWERS`) keeps today's
behaviour. Not persisted: questions still waiting when the app closes are not sent and not charged (owner may object).
