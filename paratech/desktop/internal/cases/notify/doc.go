// Package notify is the use case of the "processing complete" notification (PRD section 15): when a job of a
// user-visible processing step finishes (COMPLETED or FAILED), the user is notified, if the setting is on.
//
// At most once per job and status, also across restarts: the notice is recorded first and the notifier is called only
// by the caller that recorded it (claim before send, like the scheduler). A crash or a failed notifier between the two
// loses that notification; it is never repeated. The decision itself is domain/jobs/services/notifypolicy.
//
// The service is fed by the job runner (jobrunner.Observer, satisfied structurally). Rules that apply: ARCH-20..27.
package notify
