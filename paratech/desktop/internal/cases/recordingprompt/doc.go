// Package recordingprompt is the use case of the "lesson is starting" popup (PRD section 6.1, AC-01, AC-02): it turns
// a scheduler announcement into an offer to record, shows the non-modal popup without touching the main window, and
// carries out the user's choice: start the recording or skip (no snooze: owner decision 2026-10-18).
//
// LessonStarting is the consumer of the scheduler (scheduler.Events). With automatic prompts off it drops the
// announcement for good (owner decision 2026-10-14: the scheduler does not repeat it). OfferAdHoc is the tray's
// "Show recording popup": an offer without a lesson that ignores the setting (an explicit user action). One prompt is
// current at a time, in memory only; a newer lesson replaces it. The actions name the prompt ID they were shown for,
// so a click on an old popup never acts on a newer prompt.
//
// Start refuses until the Recordings port is Ready (the startup cleanups ran and the recovery marked the interrupted
// recordings, ADR-0009); the popup then says to try again shortly. Run (no goroutines of its own) hides a lesson
// prompt when the lesson ends. No toasts: the popup is the only notification of this flow.
//
// Sources is the source picker of the popup (TASK-059): the open apps from the AudioApps port and the source to
// preselect. Start records the source the user picked and, once it runs, remembers it in the settings by app name.
//
// Rules that apply: ARCH-20..27. The prompt's states and rules live in the domain/recording/prompt aggregate.
package recordingprompt
