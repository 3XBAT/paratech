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
