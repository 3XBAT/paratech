// Package job is the aggregate of a persistent background job (PRD sections 7.1 and 7.2): the status machine as a
// table (transitions.go), attempts, the idempotency key and the next run time. Time is always a parameter.
// Value objects: ClaimFilter (which QUEUED jobs a worker may claim), Progress (what a handler reports) and Snapshot
// (the read-only copy a handler receives). See ADR-0004.
package job
