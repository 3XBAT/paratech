// Package quota is the template bounded context for server-side LLM quota accounting (PRD section 9.2).
// It only documents the context and its ubiquitous language; aggregates (domain/quota/account, domain/quota/reservation)
// are added by later tasks.
//
// Layout of a context (docs/architecture.md section 3.1):
//
//	quota/doc.go                 this file: purpose and glossary
//	quota/<aggregate>/           one package per aggregate
//	quota/values/                value objects shared by several aggregates of the context
//	quota/services/<service>/    domain services and policies
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - Account: the balance, reservations and usage journal of one tester (identified by an API key).
//   - Reservation: quota set aside before an expensive LLM request starts.
//   - Settlement: closing a reservation against the actual usage reported by the provider.
//   - Release: returning the unused part of a reservation on cancellation or failure.
//   - Idempotency key: a client-supplied key that prevents double charging for a repeated request.
package quota
