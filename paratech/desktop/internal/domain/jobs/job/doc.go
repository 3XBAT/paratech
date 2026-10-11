// Package job is the aggregate of a persistent background job (PRD sections 7.1 and 7.2).
//
// TASK-003 skeleton: only the identity, the job type and the status enumeration exist so that use case
// ports can name the type. The status machine (allowed transitions as a table, ARCH-14, ARCH-19),
// attempts, backoff inputs, the idempotency key and the New/Restore factories (Restore takes the stored version) arrive
// with TASK-008. Three value objects of the runner contract exist already: ClaimFilter (which QUEUED jobs a worker
// may claim, including the "Run now" override), Progress (what a handler reports to the runner) and Snapshot (the
// read-only copy of a job that a handler receives). See ADR-0004.
package job
