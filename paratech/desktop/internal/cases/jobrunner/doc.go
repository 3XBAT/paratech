// Package jobrunner is the use case of running persistent background jobs: claiming eligible jobs, executing
// them with registered handlers, retrying with bounded backoff, cancelling, startup recovery and the
// power-aware policy (PRD sections 7.1 to 7.4 and 14, AC-09, AC-10, AC-21, AC-22).
//
// The Service (service.go) is the worker pool of TASK-009: handlers by job type, claiming, per-attempt timeout,
// bounded retry, cancellation and graceful stop. Startup recovery (TASK-010) and the power policy (TASK-012) are
// not part of it yet; the WAITING_FOR_* statuses are not produced from handler errors.
//
// Rules that apply: ARCH-20..27. The status machine of a job lives in the job aggregate, not here.
package jobrunner
