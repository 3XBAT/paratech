// Package delivery is the bounded context of sending a finished artifact to a recipient by email
// (PRD sections 10 and 14, AC-14, AC-15).
// Aggregates: domain/delivery/delivery.
//
// Layout of a context (docs/architecture.md section 3.1):
//
//	delivery/doc.go                  this file: purpose and glossary
//	delivery/<aggregate>/            one package per aggregate
//	delivery/values/                 value objects shared by several aggregates of the context
//	delivery/services/<service>/     domain services and policies
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - Delivery: a record of one attempt to send an artifact to a recipient; there is no separate Submission entity (PRD section 10).
//   - Confirmation: the explicit user approval without which a delivery cannot exist (AC-14).
//   - Status: PENDING, SENDING, SENT, FAILED, UNKNOWN (see the delivery package for the origin of the set).
//   - UNKNOWN: the outcome is ambiguous (timeout after the provider may have accepted the mail); it is reconciled, never blindly retried (hard rule 7, AC-15).
//   - Reconciliation: finding out whether an UNKNOWN delivery was actually accepted, by its Message-ID.
//   - Idempotency key: makes creating and sending the same delivery safe to repeat.
package delivery
