# TASK-326 — desktop on catalog 10

## Done
- `values.CatalogVersion = CatalogWave10` (10); stale comments fixed. v10 is a superset of v9: calls and plans stored under catalog 9 still restore; `list_assignments` always answers in the v10 form (rows carry `done`).
- Golden `desktop/tests/agent_catalog_test.go` reads `shared/testdata/tool_catalog_v10.json` (also compares `max_items`); `TestToolCatalogV10IsV9PlusTheWave`; `TestPlanArgumentsLimitOfCatalog10` (200-step plan 8–32 KiB parses, over 32 KiB refused, other tools keep 8 KiB; the 32 KiB limit already existed in `values/args.go` since TASK-317).
- Old requests on catalog 9: `TestAgentRequestOfThePreviousCatalogAfterTheUpdate` (9 → 10): «Продолжить» → `ErrRetryRequired`, «Повторить» → `StopFailed` twice, no hang, a new request works (as TASK-184).
- `TestCatalog10OnTheAgentUseCases` (real use cases + SQLite + Hub client): delete with a Google event → class C card `delete_assignment`, without an event a U write; a plan of 12 wave-10 writes: one «Да», runs to the end, one Hub call per step.
- agenteval level 1: `CatalogFile` = v10; case fields `args`, `once`, `never` (validated and scored); cases `v01`–`v10` (schedule, mark done once, verbatim note, set_setting, recording start, offer_button, delete card, 12 writes → `show_plan`, injection in a pasted schedule and in a note text; both forbid recording/set_setting/sends). `MaxRequests` 70 → 80 (72 requests now).
- `docs/agent-parity.md`: date line, wave legend (10 = done, desktop sends 10 since TASK-326), §11 wave-10 line. Layout kept.

## Owner decision recorded
2026-11-10: catalog 10 ON («Да, запускай» to «агент может всё»), +~2.2k prompt tokens per step accepted. This answers the open price question (ADR-0038 D-1); the 2000-token bound of ADR-0033 §6 is not a stop condition for this switch.

## Prefix tokens (offline estimator `hub/tests/agent_prefix_test.go`, one run)
| | system | tools | prefix | stricter |
|---|---|---|---|---|
| v9 | 6 512 | 4 046 | 10 558 | 12 509 |
| v10 | 7 560 | 4 854 | 12 414 | 14 702 |
| delta | | | +1 856 | +2 193 |

The real provider count (`count_tokens` / `usage.input_tokens`) was NOT taken: it needs a live Hub call with a tester key (none available, the owner's Hub was not called). The level-2 live eval is not run for the same reason.

## Decisions to confirm
- Expected classes of existing eval cases changed by the v10 rule «up to six writes run at once»: `p01` (subject + folder) and `p03` (folder + file) now `write`, `plan` also accepted; `p06` stays `plan`, `write` also accepted. `p02` (lecture text, paid, AlwaysPlan) stays `plan` (reviewer medium, fixed).
- Hub prompt v10 does not say that lecture text / turnkey writing always needs `show_plan` (the desktop refuses it outside a plan, ADR-0021). The eval would catch it at level 2.

## Follow-ups
- Level-2 live eval and the real prefix count (tester key, live Hub).
- Dead v9 branch in `input/agent/tools.go` `listAssignments` and stale comment in `assignment_list.go` (`listsByState` always true): delete.
- Hub prompt: lecture text / turnkey writing always via `show_plan` (TASK-325 scope).
- Two frontend fixture comments (`agentwave10cards324*.test.mjs`) still say the catalog is not served.
- Security Lows: `note.text` is not checked against the student's message (local, class U, «Отменить»); approved plan steps leave `note.text` / `change_assignment.note` free; a crafted ICS subject name could reach the planner via `list_subjects` after the student confirms the subject (single U writes with «Отменить»).
- Wave security review of docs/process/security-review.md: this task's audit covered the exposed trust boundary, no high/medium.

## Changed existing tests
`values/tool_test.go` (pin `!= CatalogWave10`); `values/wave10_test.go` (`TestCatalog9SeesNoWaveTool` kept, re-expressed for ToolsAt(9) vs current; `TestStoredDataOfCatalog9Restores` added); `input/agent/recording_test.go`, `notes_test.go` (absent at 9, served now); `input/agent/assignment_done_test.go`, `propose_test.go` (no-state call now answers in the v10 form); `hubclient/agent_summary_test.go` (current request is catalog 10); `desktop/tests/agent_injection_test.go` (sweep covers the wave tools, `sweepArgs`, `KindNote`, bool baseline; no assertion weakened); `agenteval/score_test.go` (mutation strings updated for the new `never` lists). Comment-only: `thread/decide_test.go`, `assignment_wave10_fakes_test.go`.

## Security
security-auditor (opus): no high/medium; Lows above. Policy classes of wave tools match ADR-0033 §10, Google-event delete fails closed, plans cannot smuggle C actions, set_setting closed enums, offer_button typed IDs only, no note/schedule text in logs. Reviewer: one medium (p02), fixed; two lows above.
