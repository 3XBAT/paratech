# TASK-245 — desktop on catalog 9: mentions go to the Hub (IDs only)

## Done
- `values.CatalogVersion = 9`; `TestToolCatalogV9KeepsTheToolsOfV8` pins v9 = v8 except the version line; golden `agent_catalog_test.go` on `tool_catalog_v9.json`. `TestAgentRequestOfThePreviousCatalogAfterTheUpdate` uses `CatalogVersion - 1`, so it now covers 8 -> 9 (old request: «Продолжить»/«Повторить» honest refusal).
- `values.StepMention{Ref, Current}` (no label), `StepRequest.Mentions`, `HistoryEntry.Mentions []Ref`. `hubclient` fills `AgentScope.mentions` (kind enum, id, current); refuses locally an unknown kind, empty ID, repeated object, > 16, > 8 current. Test on proto and protojson output: the label is never present.
- Rule in domain `thread.StepMentions`: current mentions first (<= 8), then the distinct mentions of the earlier requests whose pairs `Window` really sends (newest request first), total <= 16; dropped, summary-covered, before-`historyFrom` and cut-off requests give none. `Window` and `StepMentions` share one `layout()` (test over 2..40 requests).
- `agentturn` step: every step re-checks each mention through the `Mentioned` port (missing one is dropped; an error stops the request offline before the Hub call; nil port keeps no earlier mention), and puts the result both in the request and in `c.context` (provenance) — one source. The summary text is never a source (test).
- `hubclient.turnOf` maps `Summary` -> `history_summary` (nothing produces a summary until TASK-268; `Window` already emits the turn, without the mapping it would have been sent as empty results).
- sqlite (outside the task list): `ListAgentSteps` also selects `agent_turns.mentions`; `AgentHistory.Since` decodes them once per request into IDs (strict, a bad row fails the read). sqlc regenerated with v1.31.1. No port interface changed.
- agenteval: catalog v9, cases m01–m04 (mention of another subject, no invented ID, summary not obeyed, earlier mention is not the subject), `earlier`/`reply`/`summary`/`mentions` case fields, validated in `ParseCaseSet`.

## Decisions to confirm
1. The summary mapping in hubclient goes beyond the criterion «не отправляется здесь»: no summary exists yet, so real data is unchanged.
2. All mentions (current too) are re-checked on every step, up to 16 `Exists` calls; the cap is applied before the check, so deleted objects use up slots.
3. `Thread.known` still accepts a current mention deleted mid-request (no longer sent); harmless, the tool fails on a missing object. Comment added in step.go.
4. Deploy order: Hub with catalog 9 / panel v9 (TASK-244) first, then this desktop.

## Follow-ups
- Live agenteval and the «cache_read до и после» measurement need a Hub key: NOT done (pending, run by the owner/supervisor).
- `desktop/testenv/agenthub.go` (fake Hub) requires a `student_text` as the first turn, so m03 only checks the sent shape; let it accept a leading `history_summary` before TASK-268.
- agenteval does not score the lesson ID of `create_assignment` (m01 checks the subject only).
- Low: a bad stored mentions row fails `Since` for the whole chat (same as other corrupt rows); a retry with the same key can carry a different mention set after a deletion (as base subjects already do).

## Changed existing tests
`values/tool_test.go` (catalog 9); `agentturn/chats_test.go` `TestAskKeepsExistingMentions` (the step re-checks: asked list `[s7, s404, s7]`, stricter); `agentturn/fakes_test.go` (stores request mention IDs, `Since` sets them like the join); `sqlite/agent_test.go` `TestAgentHistory` (entries carry mention IDs); comments in `agent_catalog_test.go`; agenteval `CatalogFile` v9, runner key by request number.

## Security
Auditor: no High/Medium (label never leaves the machine; accepted set = sent set; no cross-chat leak; corrupt JSON refused; rule 3 holds). Reviewer: approve, three mediums (deleted current mention still in `known` — comment; summary mapping and sqlite files outside the list — recorded above), lows above.
