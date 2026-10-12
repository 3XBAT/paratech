// Package jobrunner is the use case of running persistent background jobs: claiming eligible jobs, executing
// them with registered handlers, retrying with bounded backoff, cancelling, startup recovery and the
// power-aware policy (PRD sections 7.1 to 7.4 and 14, AC-09, AC-10, AC-21, AC-22).
//
// TASK-003 contains only the ports (ports.go). The Service arrives with TASK-008 (job store scenarios),
// TASK-009 (worker pool), TASK-010 (recovery) and TASK-012 (power policy).
//
// Rules that apply: ARCH-20..27. The status machine of a job lives in the job aggregate, not here.
package jobrunner
