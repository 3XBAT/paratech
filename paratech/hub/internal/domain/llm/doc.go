// Package llm is the bounded context of the LLM calls the Hub makes on behalf of testers (PRD sections 4.2, 9.2, 13.2):
// which models may be used and what a provider call takes and returns.
//
// Layout (docs/architecture.md section 3.1): llm/values holds the client's completion spec and the types that cross
// the Provider port (request, completion, usage, stop reason, provider error); llm/services/modelallowlist is the
// server-side model allowlist; llm/services/requestlimits bounds one call (reserved amount, per-tester rate). For the
// agent (ADR-0023) llm/values also holds the agent step request, tool calls and results; llm/services/toolcatalog is
// the tool catalog of one version with the check of returned calls; llm/services/seal signs and checks echoed calls.
// The context has no aggregate yet.
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - Alias: the Hub-side model name a client sends (for example "default"); the only model name a client controls.
//   - Provider model id: the provider's name of a model (for example "claude-sonnet-5-5"); comes only from the
//     allowlist, never from a client.
//   - Allowlist: the server-configured map of alias to provider model id and output cap.
//   - Output cap: the most output tokens a call with the alias may request; a larger request is clamped silently to it
//     (ADR-0005(b)) and the effective value is the basis of the quota reservation.
//   - Spec: one completion request of a client (CompletionSpec): key, operation, alias, texts, requested output cap.
//   - Fingerprint: SHA-256 of a spec without its key; the only form in which the Hub keeps the request text.
//   - Instructions: trusted text written by the app.
//   - Untrusted data: user data (transcript excerpts); passed to the model as data, never as instructions.
//   - Completion: the provider's answer. Its output text is UNTRUSTED (CLAUDE.md hard rule 3, PRD section 13.3).
//   - Usage: provider-reported tokens of a call, used to settle the reservation.
//   - Transient / permanent: whether repeating the same call can help (ADR-0005(c)).
//   - Server prompt: the Hub's own prompt of an operation (INSPECT_MATERIAL, ANSWER, TRIAGE_ASSIGNMENT) or agent
//     profile; the client sends no instructions and its version stands in the fingerprint.
//   - Agent step: one planner call; the answer is 1-4 tool calls of the catalog, sealed by the Hub.
//   - Seal: HMAC by the Hub over the caller, thread, profile, catalog version and calls; an echo must carry it.
package llm
