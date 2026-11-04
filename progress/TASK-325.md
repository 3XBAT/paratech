# TASK-325 — Hub: Panel v10 (wave 10)

## Done
- `hub/pkg/prompts/agent/panel/v10/{tools.json,system.md}`: catalog 10 = the 27 tools of v9 + 10 wave tools (`propose_subjects_from_schedule`, `change_assignment`, `mark_assignments_done`, `list_notes`, `note`, `get_settings`, `set_setting`, `quota_status`, `recording`, `offer_button`), `list_assignments.state`, `show_plan` up to 200 steps. Names, argument order, types, enums and limits match `desktop/.../values/wave10.go` (a test pins them). Golden `shared/testdata/tool_catalog_v10.json` is byte-identical to `tools.json`. v1–v9 stay byte-identical (sha256 pins) and served.
- `system.md` v10 = v9 + the «fewer «Да»» rules, one `propose_subjects_from_schedule` call, one `mark_assignments_done` call with IDs from `list_assignments`, verbatim `note.text` (the student's words, no «Да»), `offer_button` for «открой»/«сохрани»/«войди в Google», recording start and stop at once, schedule rows and note texts as data. TASK-364/366/369 wording is kept (tests pin every v9 line except the six rewordings below).
- Hub validation: `values.MaxArgumentsBytesOf` (32 KiB for `show_plan` from catalog 10, 8 KiB otherwise); `toolcatalog` from catalog 10: `bool`, `max_items` on `ids` (≤ 50), `note.text` as `reply_text`, `offer_button` arguments only enum/IDs, 200-step plans, plan-step arguments typed (`reply_text` refused, enums checked, query ≤ 500). Old catalogs unchanged (tests).
- Tests: `prompts_v10_test.go`, `toolcatalog/catalogv10_test.go`, `values/agent_test.go`, `agentstep/plan_test.go` (large plan sealed/replayed once, one charge under concurrency; a v10 seal does not verify on v9), `hub/tests/agent_eval_v10_test.go` (recorded answers, one case per rule, injection in a note/schedule row not executed, verbatim note match, catalogs 1–10 served), `hub/tests/agent_prefix_test.go`.

## Decisions to confirm
- **Prefix size.** Offline estimate, v9 ≈ 10 558 → v10 ≈ 12 414 tokens: **+1 856** (3.5 ASCII / 2 other chars per token; bound 2 000, ADR-0033 §6). A stricter estimate (3.0 / 1.5) gives **+2 193**, over the bound. No tokenizer offline; the real count (`count_tokens` or `usage.input_tokens` v9 vs v10) must be taken at level 2 in TASK-326 before the desktop switches. If it is over 2 000: ADR-0033 О-11 (stop and decide).
- Six v9 lines reworded for «fewer «Да»»: `show_plan` only above six writes in one request (was: before more than one writing action); several assignments = one `create_assignment` each (was: show_plan first); tool-arguments line; read-only tools list; confirmation-tools line; «Предметы» screen line.
- `note.text` may hold an address or link (verbatim); exception stated narrowly in the prompt (an address is only text there).
- Shorter type texts in v10 only (the date type no longer says "end of that day", which also removes a v9 contradiction with the 20:00 default).
- Hub checks the schema only; which arguments go with which `action` and whether a `set_setting` value fits the setting stay with the desktop (error result).
- `hub/internal/domain/llm/services/toolcatalog/{schema,toolcatalog}.go` changed although not in the task's file list: the v10 argument checks cannot be done elsewhere.
- Hub deployment before the desktop (TASK-326); `CatalogVersion` on the desktop stays 9.

## Follow-ups
- TASK-317: Hub `MaxAgentTurns = 64` cuts a request past ≈ 31 planner steps; a 200-step plan needs its own Hub task. The planner output cap (4096 tokens) may not fit a 200-step plan in one reply.
- A note under 4000 characters can exceed 8 KiB for the whole call (many 3-byte characters, escapes) and fails as a tool error without a "split it" hint (rare, pure Cyrillic fits). Decide: express the limit in bytes or add a reason to the check.
- Plan-step arguments declared id/date/time are still any 80-char label on the Hub (the desktop parses each by its type): safe, a wasted planner step at worst.
- Security Low: `values.ValidID` accepts any non-space string ≤ 128 bytes (a URL-shaped ID in `offer_button`); the desktop resolves IDs against local data. A character-set limit at the Hub is defence in depth.
- Postgres-backed tests (`*AgainstPostgres`) skip locally without `HUB_TEST_DATABASE_URL`; CI runs them.
- Level-2 (live) eval of the new rules and injection resistance: TASK-326.

## Changed existing tests
`hub/pkg/prompts/prompts_test.go` (`TestEmbeddedPanel` expects 10 versions); `shared/contract_test.go` (current catalog is v10, tables and validator extended, `TestToolCatalogV9IsFrozen` added, `TestToolCatalogV8IsFrozen` compares with the v9 file); nothing weakened.

## Security
Adversarial review (security-auditor): no high/medium. Lows above (ID charset, plan-step loose argument names pre-existing). Seals bind the catalog version, so a large plan cannot be replayed on an older catalog; old catalogs keep the 8 KiB limit; no note text in logs.
