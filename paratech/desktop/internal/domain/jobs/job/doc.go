// Package job is the aggregate of a persistent background job (PRD sections 7.1 and 7.2).
//
// TASK-003 skeleton: only the identity, the job type and the status enumeration exist so that use case
// ports can name the type. The status machine (allowed transitions as a table, ARCH-14, ARCH-19),
// attempts, backoff inputs, the idempotency key and the New/Restore factories arrive with TASK-008.
package job
