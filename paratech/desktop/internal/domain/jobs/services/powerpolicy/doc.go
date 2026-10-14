// Package powerpolicy is the domain service of power-aware processing (PRD section 7.4, AC-21, AC-22): heavy local
// jobs such as STT wait for AC power while the user setting "process only on AC power" is on, light jobs (LLM requests,
// email, calendar) never wait, and the "Run now" override of a job lifts the wait for one start.
//
// It is pure and deterministic (ARCH-15): the power source, the setting and the time come in as parameters. An unknown
// power source counts as AC power, so only a known battery restricts anything.
package powerpolicy
