// Package jobrunner is the use case of running persistent background jobs: claiming eligible jobs, executing
// them with registered handlers, retrying with bounded backoff, cancelling, startup recovery and the
// power-aware policy (PRD sections 7.1 to 7.4 and 14, AC-09, AC-10, AC-21, AC-22).
//
// The Service (service.go) is the worker pool of TASK-009: handlers by job type, claiming, per-attempt timeout,
// bounded retry, cancellation and graceful stop. Startup recovery (recover.go, TASK-010) runs before Start and
// handles the jobs a previous process left RUNNING by the Recovery policy registered with each type; work with
// external side effects is reconciled, never requeued blindly (hard rule 7). The power policy (power.go, TASK-012)
// applies domain/jobs/services/powerpolicy: Heavy jobs wait in WAITING_FOR_POWER on battery while the setting is on,
// and ForceRun is the "Run now" override. The network and quota waits are not produced yet. An optional Observer
// (observe.go) is told about every saved terminal status, for the notification of PRD section 15.
//
// Rules that apply: ARCH-20..27. The status machine of a job lives in the job aggregate, not here.
package jobrunner
