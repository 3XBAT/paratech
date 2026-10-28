// Package preferences is the use case of the settings page (TASK-021): read the user toggles and change the
// automatic recording prompts (PRD section 6.1, AC-02) and the AC-power restriction of heavy jobs (PRD section 7.4,
// AC-22), the student profile of the homework documents (TASK-094) and the recognition of photo pages of notes with
// whether Windows can recognise Russian (TASK-294). Defaults and validation stay in the settings
// aggregate; this package loads, changes and saves it.
//
// Rules that apply: ARCH-20..27.
package preferences
