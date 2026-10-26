# TASK-200 — text-only planner answer becomes a reply card; triage re-ask; panel prompt v5

## Done
- Planner answer with stop_reason=end_turn and only text (no tool_use): the adapter keeps the text, agentstep turns it into `reply{text}` via `Catalog.TextReply` (id `hub_text_<reservation>`), checked like a returned reply plus the desktop's printable rule (no tab, `<`, `>`, U+FFFD, format chars). Otherwise the old charged failure. No assistant turn is kept for it (a continuation is ErrTurnGone). `tool_choice` stays `auto` (thinking).
- Log: `reason=no_tool_use|output_cap|invalid_tool_calls`, `text_reply`; gateway `reason=invalid_answer`, `reask=accepted|refused|failed|no_time`; Connect message for an invalid answer differs from "the LLM provider failed".
- Triage: prompt v2 demands the helper; on a missing helper (`triage.ErrMissingHelper`) the Hub asks once more inside the same reservation (note in `ops/triage_assignment/v2/reask.md`, loaded by `app`, `gateway.ServerPrompt.Reask`), both usages charged once via Settle. Recorded in ADR-0025 addendum.
- Panel prompt/catalog v5 (tools as v4; golden `shared/testdata/tool_catalog_v5.json`): "always reply, even to a general question", only existing screens, no import of a ready recording.

## Decisions to confirm
- Plain text over 300 characters (or with `<`/`>`) still fails as before (criterion allows); cutting to 300 is an owner choice.
- A failed second call of the re-ask is charged only for the first usage (ADR-0010 gap).

## Follow-ups
- DESKTOP: `values.CatalogVersion` is still 4, so panel prompt v5 is not served until the desktop moves to 5 (constant, golden file reference, agenteval CatalogFile). Text-reply and re-ask are live for all catalogs now.
- Desktop UI text for the remaining "no tool use" / invalid-answer failures is unchanged (separate UI task).
- Lows: no app-level test that the wired triage ServerPrompt has a non-empty Reask; TextReply does not trim surrounding whitespace like the desktop (safe fallback); hub/tests agent eval runs without Reask.

## Changed existing tests
prompts_test (versions 1..5, triage v2), shared contract_test (catalog v5, v4 frozen), toolcatalog_test, connect gateway_test (new message), gateway triage_test fixture (Reask), missinghelper/reask tests after the note moved out of the domain.

## Security
Auditor: no findings. Reviewer: changes requested (note in domain, desktop-stricter text), fixed in 0c34ee07, re-review approve.
