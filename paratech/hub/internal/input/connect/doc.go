// Package connect is the Hub's HTTP transport: a chi router with the Connect-RPC handlers of shared/gen, the
// liveness probe /healthz and the readiness probe /readyz (docs/architecture.md section 6).
//
// The router is the public, API-key listener. Of QuotaService only GetRemaining is reachable here; Reserve, Settle and
// Release are Hub-internal (ADR-0005(f)) and answer 404 as if they did not exist.
package connect
