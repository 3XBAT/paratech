# TASK-244 — Hub panel v9 / catalog 9: chats, mentions, summary turn, cache layout

## Done
- Proto (additive, buf breaking green): `AgentScope.mentions = 5` (`AgentMention {kind, id, current}`, enum `AgentMentionKind`), `AgentTurn.history_summary = 5` in the oneof. Comments about "one thread per subject" / "only the current subject" updated.
- Hub values/case: mentions checked (known kind, ID format as other scope IDs, <= 16); `history_summary` only on catalog >= 9, only as the first turn directly before a `student_text`, <= 8 KiB, not blank, valid UTF-8. Mentions or a summary on catalog < 9 -> invalid_argument. Both count in request bytes and `InputBytes`; mentions enter the idempotency fingerprint only for catalog >= 9 (old fingerprint pinned by a test).
- Anthropic layout for catalog >= 9: tools -> system -> base (breakpoint 1) -> summary + earlier pairs (breakpoint 2 on their last block) -> scope + mentions block (JSON data, "Current request") -> current request (breakpoint 3). Old catalogs: byte-identical layout (golden files recorded from the unchanged adapter).
- `panel/v9/system.md` = v8 + "Chats" section + 20:00 phrase; "thread" reworded to "chat / current request". `tools.json` v9 = v8 except the version line; `shared/testdata/tool_catalog_v9.json` identical. v1..v8 still served; app wiring needed no change.
- Review fixes: subject/lesson from mentions only when `current` is true (an earlier-request mention helps only for «там», «то же»); "mentions no subject" wording; proto comment on duplicates; cache test across requests.
- Tests: values, agentstep, anthropic layout, connect mapping, prompts, shared contract, level-1 evals (mention of another subject, no invented ID, summary not obeyed, earlier mention is not the subject, chat fields shape).

## Decisions to confirm
1. **No Hub-side provenance check.** The task/ADR-0028 say mentions "enter the provenance check like scope refs", but the Hub has none (ADR-0021/0023: the typed check is the desktop's `seen`). Hub validates, fingerprints and renders mentions and tells the model they are an ID source. TASK-245 wording «так же, как Hub» should read «как ссылки области в проверке десктопа»; the summary text must never feed `seen`.
2. Mentions / summary on catalog < 9 are refused (strict).
3. No duplicate check on mentions (harmless; desktop sends distinct ones).
4. **Prompt conflict:** system.md says a date without time is 20:00 (owner 2026-10-27), while the `date` type description in tools.json (unchanged, by the task) still says "end of that day". The model sees both. Fix the type text in the next catalog (TASK-325 v10) or confirm that system.md wins.
5. Cache reuse is by design partial: request N+1 reuses tools, system, base and the earlier pairs up to request N's second breakpoint; the scope + request N block of call N is not reused (the scope sits before the current request). Inside one request every tool step reuses the previous one.

## Follow-ups
- Deploy: Hub with v9 before the desktop on catalog 9 (TASK-245).
- `MaxAgentTurns` 64 untouched (desktop allows 60 planner steps under a plan; TASK-325 raises limits).
- Low: Unicode format characters (bidi, zero-width) pass `ValidID` in IDs like the other scope IDs (JSON-encoded, from the tester's own client). Hardening only.
- Postgres-backed Hub tests need a DSN; skipped locally, CI runs them.
- Level-1 evals do not prove model behaviour; live check is in TASK-245 agenteval.

## Changed existing tests
`TestEmbeddedPanel` (9 versions); `shared/contract_test.go` (field tables, enum list, catalog version/file -> 9; `TestToolCatalogV7IsFrozen` compares with the v8 file, new `TestToolCatalogV8IsFrozen`); `TestAgentEvalHubV9MentionOfAnotherSubject` looks up the reworded rule.

## Security
Auditor: no high/medium. Summary is JSON-escaped data in a user block, never in system; mentions are closed-enum kind + validated ID; no downgrade path; seal binding unchanged; no new log of content. Low: format characters in IDs.
