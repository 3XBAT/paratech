# TASK-112 — ADR-0021…0024, architecture.md for "agent as core"

Done (documents only): ADR-0021 agent core, 0022 untrusted input, 0023 Hub AgentStep, 0024 subjects/library;
README index (all ADRs); architecture.md (§2, §6, §12, ARCH-04, pointers in ARCH-27/ARCH-41); docs/design/concept-agent.html.
"Amended by" notes added to ADR-0005, 0010, 0019. Changed existing tests: none.

Decisions to confirm
- ADRs are marked Accepted (autonomy mandate).
- Seal is bound to a `thread_id` field in the AgentStep request (proto task must carry it).
- Until previous versions are kept (D7), replacing a ready result is class C (confirm), not U.
- Plan: counted over the whole request; at most 1 paid job without plan; approved steps run once.
- Where a replaced result's old version is kept (D7) is open (ADR-0021).

Follow-ups
- TASK-114: check min cacheable prompt length/prices in docs/refs/anthropic.md.
- TASK-121: mapping of existing free subject strings to subject IDs.
- Test: input/agent interfaces expose no Confirm/Execute/Delete/Send (in ADR-0021).
- Security: eval level-1 fixtures for ICS title, triage text, error text.
