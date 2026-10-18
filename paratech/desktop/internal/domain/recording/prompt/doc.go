// Package prompt is the aggregate of one offer to record a class (PRD section 6.1, AC-01): the "lesson is starting"
// popup, or an offer the user asked for without a lesson (ad hoc).
//
// A prompt has a typed state machine (transitions.go): OFFERED (the popup shows it) -> STARTING (a start runs) ->
// STARTED; a failed start goes back to OFFERED with a Problem; OFFERED -> SKIPPED (the user declined) or EXPIRED (the
// lesson ended). There is no snooze (owner decision 2026-10-18). The rules live here: only a lesson prompt expires;
// one start at a time. A prompt is kept in memory only: after a restart its lesson's prompt window has passed anyway.
//
// The lesson is copied in as primitives and shared IDs (ARCH-11: no import of the schedule context). Time is always a
// parameter (ARCH-15).
package prompt
