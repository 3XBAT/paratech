// Package quota is the bounded context of server-side LLM quota accounting (PRD section 9.2, AC-11, AC-12).
//
// Layout (docs/architecture.md section 3.1): quota/account is the one aggregate of the context, the account of a
// tester with its reservations and idempotency keys (ADR-0008 resolves ADR-0005(h) this way; a reservation is an
// entity of the account, not an aggregate of its own).
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - Account: the used and held totals, reservations and usage journal of one tester (identified by an API key).
//   - Limit: the allowance of every tester, from the Hub configuration; it does not renew in MVP.
//   - Reservation: quota set aside before an expensive LLM request starts; one per attempt of an idempotency key.
//   - Settlement: closing a reservation against the actual usage reported by the provider.
//   - Release: returning a reservation whole on cancellation, failure or expiry.
//   - Idempotency key: a client-supplied key, namespaced per tester, that prevents double charging for a repeated
//     request; its fingerprint (operation, model alias, amount) detects reuse with another request.
//   - Ledger: the append-only list of movements (reserve, settle, release, expire) of an account.
package quota
