# ADR-0008: Hub quota ledger

- **Status:** Accepted
- **Source:** TASK-033; resolves ADR-0005(h) and PRD section 19, decision 4 (for MVP)

## Context
The Hub must reserve quota before a provider call, settle it against actual usage, release it on cancel or failure, and
never charge twice for one idempotency key (PRD section 9.2, AC-11). Reserve and Settle change both the balance and a
reservation, which ADR-0005(h) left open as "one aggregate or an exception to ARCH-23".

## Decision
- **Unit and limit from config.** `HUB_QUOTA_UNIT` accepts only `tokens` (input and output together). `HUB_QUOTA_LIMIT`
  (default 1 000 000) is one allowance per tester, not renewing (`period_ends_at` unset), applied when the account is read:
  a new value applies to everyone, the DB stores only `used` and `held`. Remaining is `max(0, limit - used - held)`.
  Per-tester limits, top-ups and periods are follow-ups.
- **One aggregate.** `domain/quota/account` is the account of one API key with its reservations (entities) and idempotency
  key records. So ADR-0005(h) needs no ARCH-23 exception. Reservation states follow quota.proto, with a transition table:
  RESERVED -> SETTLED | RELEASED(explicit, failed, expired); RELEASED(expired) -> SETTLED once (late Settle). The
  aggregate emits one ledger movement per change of the totals (reserve, settle, release, expire, with held/used deltas).
- **Partial load.** A command loads the totals plus only what it needs: the overdue RESERVED reservations (at most 100,
  oldest first), the key record with its latest attempt, or the addressed reservation. Every query is scoped by API key,
  so another tester's reservation is `not found`. Overdue holds beyond the 100 still count as held until later commands
  release them, so remaining is under-reported and Reserve may refuse (errs toward denying); the sweeper fixes this.
- **Transactions.** One use-case call is one Postgres transaction: `INSERT ... ON CONFLICT DO NOTHING` of the account row,
  `SELECT ... FOR UPDATE` on it (serializes all commands of one tester), load, run the aggregate, write reservations, key
  record, ledger rows and totals. Any error rolls everything back, so QUOTA_EXCEEDED stores nothing. CHECK, UNIQUE
  (api key, key, attempt) and FK constraints back the invariants. Each transaction is bounded by 5 s.
- **Charging.** Settle charges `min(actual, max(hold, what the account can bear))`: the granted hold is always chargeable,
  anything above it only while quota is left. A late Settle after expiry has no hold and is bounded by what is left.
- **Expiry** is lazy: every mutating command first releases the overdue holds of the account. `GetRemaining` writes nothing
  and counts overdue holds as released. `HUB_QUOTA_RESERVATION_TTL` (default 10m, 1m..1h) must exceed the longest gateway
  request (provider call plus settlement).
- **Retention.** `HUB_IDEMPOTENCY_RETENTION` (default 24h, 1h..24h) counts from when the key's fingerprint was recorded. After
  it the same text is a new key with a fresh fingerprint; attempt numbers keep counting and old reservations stay in the
  ledger, never replayed. A key whose latest attempt is still RESERVED and unexpired stays live past the retention (at
  most retention + TTL), so one key never holds twice. The fingerprint is operation, model alias and amount: no request
  text. ADR-0005(d) "live at most 24h" is read as: not used after the retention except for that case; rows are kept
  without request text until a sweeper deletes them.
- **Exposure.** Only `GetRemaining` is served (caller = authenticated API key). Reserve, Settle and Release stay internal: the
  router answers 404, the handler embeds the unimplemented ones. The proto is not split.

## Consequences
- The gateway task calls the ledger in-process. ARCH-20 forbids importing `cases/quota`, so `cases/gateway` declares its
  own port of the same shape; the same Postgres adapter and the same aggregate serve both.
- `docs/architecture.md` section 12 still lists `reservation` as an aggregate; read it as an entity of `account` (to be
  updated in a separate docs change).
- For the gateway task (TASK-035):
  - the Reserve fingerprint has no request-text hash; per ADR-0005(d) the gateway's key record compares a hash of the
    request text, so the same key with another text is IDEMPOTENCY_CONFLICT before any stored result is replayed;
  - reserve at least the maximum possible charge (input tokens + clamped `max_output_tokens`), and keep the request
    timeout plus settlement below `HUB_QUOTA_RESERVATION_TTL`; otherwise usage above the hold or after expiry may go
    uncharged;
  - anchor the gateway key record (stored result) to the same retention start as the ledger key, or handle a ledger replay
    of a SETTLED reservation whose result is already gone.
- Follow-ups: a sweeper for overdue holds and stale key records (rows older than the retention are only overwritten, not
  deleted); a DB guard that keeps `quota_ledger` append-only; Connect error mapping for Reserve/Settle/Release (ADR-0005(c))
  when they get a transport; a config change that lowers the limit below usage shows remaining 0 but keeps the charges.
