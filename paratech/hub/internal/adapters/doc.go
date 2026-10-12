// Package adapters is the outbound layer root: one package per technology or external system
// (postgres, anthropic, ...), see docs/architecture.md section 5.
//
// Rules that apply: ARCH-30 (imports domain/**, std, internal/metrics, pkg, third-party and shared/gen; never cases, input,
// other adapters or app), ARCH-31 (no business logic), ARCH-32 (translate external errors),
// ARCH-34 (no secrets in logs), ARCH-35 (generated code stays inside the adapter or shared/gen),
// ARCH-36 (shared/gen types stop at the adapter).
package adapters
