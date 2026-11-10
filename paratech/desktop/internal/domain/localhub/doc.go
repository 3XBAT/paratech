// Package localhub is the bounded context of the Hub the owner's beta starts on this computer (ADR-0036). It is beta
// tooling: the code is ordinary and tested in every build, but only the wiring behind the build tag beta uses it, so
// the student exe never contains it. There is no aggregate: the context is value objects and one rule.
//
// Layout of a context (docs/architecture.md section 3.1):
//
//	localhub/doc.go                  this file: purpose and glossary
//	localhub/values/                 Target, ServerSettings, Facts, Status, Reason, NextStep, Diagnosis, SecretName
//	localhub/services/diagnosis/     Diagnose: what the probe facts mean (ADR-0036 section 4)
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - Target: the loopback port of the desktop's «Адрес сервера» (http, 127.0.0.1 or localhost, explicit port). The
//     local Hub always listens on 127.0.0.1:<port>. Any other address means a remote Hub: nothing is started.
//   - Server settings: the non-secret Hub settings (provider base URL, auth header, quota limit); zero values mean the
//     Hub default.
//   - Secret slot: a named secret of the Hub kept in the secret store (provider key, database URL, seal keys). The
//     domain knows only the names; values never pass through it.
//   - Facts: what a probe found (secrets present, hub.exe present, /healthz, /readyz, Docker engine, database port,
//     whether the database container is startable, who listens on the port, last exit of the Hub this desktop
//     started) plus the use case's memory of a failed container start. Facts never carry a secret value.
//   - Diagnosis: the status shown to the owner, its reason and the next step, derived from the facts; it also says
//     whether the app may start the database container (CanStartDatabase).
//
// The app never starts Docker Desktop (owner rule): Docker off is a reason with a next step for the owner. When the
// engine already runs and the database does not answer, the app starts the database container itself if it can
// (ADR-0036 section 8, О1); a failed start is the reason database_start_failed.
package localhub
