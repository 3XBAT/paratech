# ADR-0005: Hub contract decisions (gateway, quota ledger, errors)

- **Status:** Accepted
- **Source:** TASK-004 (`shared/proto/paratech/hub/v1`) and its review

## Context
TASK-004 defined the client-Hub contract: `GatewayService` (Complete, CompleteStream), `QuotaService` (Reserve, Settle, Release,
GetRemaining), `ErrorCode`/`ErrorDetail`. The task text left several points open and the review found places where the first draft
contradicted itself (the idempotent Reserve could never be retried after a failed call; a cancelled stream gave free output). The Hub
tasks (quota ledger, gateway, interceptors) and the desktop Hub client (TASK-005 and later) implement this contract, so the decisions
are recorded here and the proto comments point to them. The proto files are the normative text; this ADR gives the reasons.

## Decision

### a. Quota unit is tokens, for now
`QuotaAmount` is `{unit, value}` with a single unit, `QUOTA_UNIT_TOKENS` (input and output tokens counted together), because the
final unit is still open (PRD section 19, decision 4). The explicit `unit` field lets weighted credits or processing time be added
later without changing message shapes. Amounts are `int64`; floating point is forbidden in the contract (checked by
`shared/contract_test.go`).

### b. Payload shape: `instructions` + `untrusted_data`
A request carries trusted app-written `instructions` and, separately, user data in `untrusted_data` (PRD section 13.3). The Hub passes
the latter to the model as data. Limits mirror the desktop value object (`values.NewLLMRequest`): instructions non-empty, valid UTF-8
without NUL, `len(instructions) + len(untrusted_data) <= 1 MiB`, model alias 1-64 characters from `[a-z0-9_-]` starting with a letter or
digit, idempotency key 1-128 printable ASCII characters without spaces. The Hub enforces them again (`invalid_argument`) and bounds
the message with `connect.WithReadMaxBytes` (1 MiB plus envelope headroom). Where the numbers differ in the future, the proto is aligned
to the desktop. `max_output_tokens` is clamped silently to a per-alias cap and is the basis of the reservation.

### c. Error codes and Connect mapping
The four codes of the task (QUOTA_EXCEEDED, RATE_LIMITED, INVALID_API_KEY, PROVIDER_ERROR) are extended with
`ERROR_CODE_IDEMPOTENCY_CONFLICT` (same key, different request), because the client must tell a bug in its own key use from a
transient failure. The code travels as `ErrorDetail` attached to the Connect error. Mapping: QUOTA_EXCEEDED and RATE_LIMITED to
`resource_exhausted`, INVALID_API_KEY to `unauthenticated`, PROVIDER_ERROR to `unavailable`, IDEMPOTENCY_CONFLICT to
`failed_precondition`. Details that need no code keep a plain Connect code (`invalid_argument`, `aborted`, `not_found`, `internal`).
`ErrorDetail.retryable` says whether repeating the same request can help (a PROVIDER_ERROR may be permanent: context too long, a
refusal). When the detail is stripped (proxy, student network, transport limit) the client falls back on the Connect code:
`resource_exhausted` without detail is treated as rate limited (backoff, bounded retries, `GetRemaining` can tell a real quota shortage),
`unauthenticated` as invalid key, `unavailable` and `aborted` as transient. The `ErrorDetail` round trip is tested through the generated
handler and client in both wire codecs.

### d. Idempotency keys and reservations
- Keys are namespaced per authenticated tester (API key). Records of a key, including a stored result, live at most 24 hours
  (configurable on the Hub); the Hub keeps no model output or other transcript-derived text longer (PRD sections 4.2 and 13.2). The
  request text is kept only as a hash (fingerprint) for conflict detection.
- A key has a fingerprint and a list of attempts; each attempt owns one reservation. Reserve with an existing key replays only a
  RESERVED (unexpired) or SETTLED-complete reservation. If the latest reservation is RELEASED (explicitly, after a failed call, or by
  expiry), or was settled as partial usage, or the earlier attempt failed with QUOTA_EXCEEDED, Reserve opens a new attempt with a new
  reservation_id. This is what makes the Gateway rule "a call that failed before the provider answered is retried afresh with the same
  key" possible. A key with a different fingerprint is IDEMPOTENCY_CONFLICT in every state.
- Settle and Release address one attempt by `reservation_id` and never touch other attempts of the key. They verify that the
  reservation belongs to the caller (API key scope); a foreign id is `not_found`.
- `SettleRequest.partial` marks usage consumed by a call that did not complete; such an attempt stores no result and the retry is a new,
  separately charged attempt.
- `expires_at` must exceed the longest request timeout. Settle of a reservation released by EXPIRY still succeeds (the work was done,
  the usage is charged, bounded by what the account can bear); Settle of a reservation released by an explicit Release is
  `failed_precondition`. The Hub keeps the release reason internally; it is not part of the contract.

### e. Stream semantics
`CompleteStream` delivers zero or more deltas, then exactly one result. A failure is a Connect error with an `ErrorDetail`; the client
discards deltas of a failed stream. Provider or Hub failure releases the reservation and charges nothing. A client cancel after the
provider was called and produced output settles the tokens consumed so far (provider-reported usage for the interrupted call, else an
estimate; never above the reservation; `partial = true`; on a context detached from the cancelled request) so that a cancel never
gives free output (PRD section 9.2 step 4); the reservation is released whole only if the provider was never called or produced nothing.
If the provider had already finished, the call completes normally and its result is stored. For unary `Complete` the Hub lets a started
provider call finish and stores the result, so a retry replays what was paid for. Accepted gap: a provider failure after output was
generated is not charged.

### f. Exposure of Reserve, Settle and Release
`QuotaService` keeps its four RPCs in one service (the task's acceptance criteria name them). Reserve, Settle and Release are INTERNAL:
only the Hub's own gateway path may call them (Settle takes provider-reported usage; a client must not dictate it). Only
`GetRemaining` is registered on the public API-key listener, next to `GatewayService`. `shared/contract_test.go` pins the public and
internal sets in a table, so moving an RPC is a conscious edit. Follow-up option for the Hub quota task: split the internal part into a
separate `QuotaLedgerService` so that the public surface is exposed by service rather than by registration; this is a breaking
change and is cheapest before the first release.

### g. `buf breaking`
The task's command `buf breaking --against '.git#branch=HEAD~1'` does not work in this layout: `HEAD~1` is not a branch and the git root
is the parent of `paratech/`. The working form, run from `paratech/shared`, is
`buf breaking --against '../../.git#ref=HEAD~1,subdir=paratech/shared'` (documented in `paratech/README.md`; exit code 100 on breaking
changes). Breaking detection is NOT enabled in CI yet: the `master` baseline still contains `health.proto`, which this change removes,
so enabling it would fail this PR. Follow-up, right after this change reaches `master`: set `breaking: true` in the buf-action step
of `.github/workflows/ci.yml` with the pull-request base as baseline. Another follow-up idea, a CI-safe stale-file check: `buf generate` does
not delete generated files of a removed proto, so the existing drift check misses a stale `.pb.go`; the check should run
`rm -rf shared/gen && buf generate` before `git status --porcelain -- shared/gen`.

### h. Ledger consistency (to be decided in the Hub quota task)
Reserve and Settle change two records, the account balance and the reservation, which ARCH-23 would call two aggregates in one
transaction. Whether the ledger is modelled as one aggregate (account with its reservations), or as a documented exception to
ARCH-23 with a single Postgres transaction, is decided in the Hub quota task, with its own ADR if the rules are touched. The contract
does not depend on the answer.

## Not done (follow-ups from the review)
- A schema or structured-output request field on `CompletionSpec` (the desktop validates output locally; revisit with TASK-041).
- The desktop port for `GetRemaining` (AC-12, user can see remaining quota) belongs to the desktop Hub client tasks.
- `max_output_tokens` is `uint32` while token counts elsewhere are `int64`; kept, values are small and bounded by the Hub cap.

## Consequences
- The Hub quota and gateway tasks implement the key, attempt and partial-settlement rules above and test them (concurrent Reserve with
  one key, retry after PROVIDER_ERROR and after expiry, cancel mid-stream, Settle after expiry, foreign reservation id).
- Adding a free-text field, a message, an RPC, or changing the exposure of an RPC fails `shared/contract_test.go` until the table in the
  test is edited consciously.
- The desktop client must treat the Connect code as the fallback classification when no `ErrorDetail` is present.
