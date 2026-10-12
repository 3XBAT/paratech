// Package settings is the aggregate of the application settings (PRD sections 6.1, 7.4 and 15,
// AC-02, AC-07, AC-22).
//
// TASK-003 skeleton: only the read accessors for the settings named by the PRD exist so that use case
// ports can name the type. There is no constructor yet: the zero value has every switch off, which is NOT the
// default (the defaults are recording prompts on, process only on AC power on, notifications on). Defaults,
// New/Restore factories and the change methods arrive with TASK-007; do not rely on the zero value.
package settings
