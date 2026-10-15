// Package activity is the read side of the recordings and jobs screen (TASK-023): the newest recordings with their
// status and duration (PRD section 6.2) and the newest jobs with their status and waiting reason (PRD sections
// 7.2-7.4). What the screen offers ("Stop", "Run now") is asked from the aggregates; the actions themselves belong
// to the recording session and the job runner.
//
// Rules that apply: ARCH-20..27.
package activity
