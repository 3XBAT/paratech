// Package domain is the root of the domain layer (docs/architecture.md section 3).
//
// Layout: domain/shared (shared kernel) and domain/<context>/<aggregate> (one package per aggregate),
// plus domain/<context>/values and domain/<context>/services/<service>.
//
// Rules that apply: ARCH-10 (std, own context and domain/shared only), ARCH-11 (contexts never import each other),
// ARCH-12..14 (one package = one aggregate, references by ID, state changed only by aggregate methods),
// ARCH-15 (no I/O, goroutines, time.Now or randomness), ARCH-16..19 (events, services, errors, state machines).
// Checked by depguard in the workspace .golangci.yml and by tests/arch_test.go.
package domain
