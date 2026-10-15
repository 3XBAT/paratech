// Package notice is the aggregate of one "processing finished" notification (PRD section 15): the fact that the user
// was notified that one job reached one finished status. Its identity is the job and the status, so a store that keeps
// one notice per identity lets the notification be sent at most once for the same job state, also after a restart.
//
// Only COMPLETED and FAILED are notifiable: processing finished either way. A cancelled job was stopped by the user,
// who needs no notification about it. A notice holds no user content: the texts shown are fixed per job type.
package notice
