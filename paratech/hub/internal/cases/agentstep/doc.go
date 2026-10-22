// Package agentstep is the agent planner step use case (ADR-0023, gateway.proto AgentStep): rate limit the tester,
// check the request limits, the catalog version and the seals of echoed calls, reserve the most the step may cost,
// call the provider with the server prompt, tools and alias of the profile, check the returned calls against the
// catalog, then settle and store them, or release. The answer carries a new seal. A retry with the same idempotency
// key replays the stored calls without a second charge. An answer the provider billed but that holds no usable calls
// (output cap, end of turn, invalid calls) is charged and stored as a failure, so its retry repeats the failure; only
// a refusal or a call without an answer is released. The key rules live in the account aggregate; every ledger
// step is one transaction (ARCH-23).
package agentstep
