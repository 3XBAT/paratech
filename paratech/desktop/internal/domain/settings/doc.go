// Package settings is the bounded context of user-adjustable application settings (PRD sections 6.1 and 7.4).
// Aggregates: domain/settings/settings.
//
// Layout of a context (docs/architecture.md section 3.1):
//
//	settings/doc.go                  this file: purpose and glossary
//	settings/<aggregate>/            one package per aggregate
//	settings/values/                 value objects shared by several aggregates of the context
//	settings/services/<service>/     domain services and policies
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - Settings: the single set of typed user settings of the installation (single user, no accounts in MVP).
//   - Recording prompts: the global switch for the automatic "lesson is starting" popup (AC-02).
//   - Process only on AC power: the switch that keeps heavy local jobs (STT) off the battery (AC-21, AC-22).
//   - Default STT model: the installed model used when a job does not pin another one (AC-07).
//
// Settings never hold secrets or tokens (hard rule 4).
package settings
