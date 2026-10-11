// Package metrics holds metrics and observability helpers. It may be imported by cases, adapters and input
// but never by domain (ARCH-10, ARCH-20, ARCH-30, ARCH-40). Metrics must not influence business logic.
// Registering metrics here is the only allowed exception to ARCH-53.
package metrics
