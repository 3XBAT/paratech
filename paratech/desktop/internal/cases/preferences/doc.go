// Package preferences is the use case of the settings page (TASK-021): read the user toggles and change the
// automatic recording prompts (PRD section 6.1, AC-02), the AC-power restriction of heavy jobs (PRD section 7.4,
// AC-22) and the notification about finished processing (PRD section 15). Defaults and validation stay in the settings
// aggregate; this package loads, changes and saves it.
//
// Rules that apply: ARCH-20..27.
package preferences
