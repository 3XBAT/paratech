// Package jobs is the template bounded context for durable background jobs (PRD sections 7.1 and 7.2).
// It documents the context and its ubiquitous language. The aggregate domain/jobs/job is a skeleton (identity and the
// status enumeration, TASK-003); its state machine arrives with TASK-008. Domain services
// (domain/jobs/services/backoff, domain/jobs/services/powerpolicy) are added by later tasks.
//
// Layout of a context (docs/architecture.md section 3.1):
//
//	jobs/doc.go                  this file: purpose and glossary
//	jobs/<aggregate>/            one package per aggregate
//	jobs/values/                 value objects shared by several aggregates of the context
//	jobs/services/<service>/     domain services and policies
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - Job: a durable record of work that must be completed; the worker is ephemeral, the job state is durable.
//   - Worker: an in-process loop that claims an eligible job and performs it.
//   - Status: QUEUED, RUNNING, COMPLETED, FAILED, CANCELLED, RETRY_WAIT, WAITING_FOR_NETWORK,
//     WAITING_FOR_QUOTA, WAITING_FOR_POWER (PRD section 7.2).
//   - Attempt: one execution try of a job; a transient failure leads to RETRY_WAIT with backoff.
//   - Idempotency key: a stable key that makes a job with external side effects safe to repeat.
//   - Startup recovery: a stale RUNNING job is moved back to QUEUED when the application starts.
//   - Notice: the user was notified that a job reached a finished status (COMPLETED or FAILED); at most one per job and
//     status (PRD section 15, aggregate domain/jobs/notice, policy domain/jobs/services/notifypolicy).
package jobs
