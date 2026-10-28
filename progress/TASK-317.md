# TASK-317 — plan budget (ADR-0033 §4)

## Done
- `values.MaxPlanSteps` = 200 (a runaway guard, comment says so); `MaxPlanArgumentsBytes` = 32 KiB for `show_plan` only in `ParseCall`, other tools stay 8 KiB. Plan parsing, the plan card and `ProposePlan` accept up to 200 steps; a 200-step plan survives `EncodePlanSteps`/`DecodePlanSteps` (sqlite form, no size limit of its own).
- `thread.ProposePlan`: the «more than MaxWrites writes» check is gone. `thread.Admit`: a step of an approved plan is not counted in `Writes` and not stopped by `MaxWrites`; a write outside the plan is `ErrNotInPlan`; without a plan nothing changed (counted, `MaxWrites`, `NeedsPlan`, `MaxPaidUnplanned`).
- `budget.go`: `MaxPlanRunSteps` = 60, `MaxPlanInputTokens` = 1 000 000, chosen by `Turn.stepLimits()` in `BeginStep`; same «лимит просьбы» stop. `valid()` accepts Steps up to 60, so old saved requests load. No migration.
- `input/agent/driver.go`: `maxSubmits = thread.MaxPlanRunSteps` (else the driver cancelled a long plan at 8 submits). `cases/agentturn` `historyLimit` enlarged to `MaxHistoryTurns*(2*MaxSteps+1) + 2*MaxPlanRunSteps + 1` (461 entries) so the current 60-step request is read whole.
- Each planner step stays its own quota reservation on the Hub (unchanged).

## Decisions to confirm
- **Plan limits apply while the request has an approved plan, also after its last step ran** (ADR-0033 §4 says «while an approved plan has unused steps»). Otherwise a successful 30-step plan would end with «лимит просьбы» at the closing reply step; one closing step cannot be granted without new persisted state. The 60 steps / 1M tokens still bound the request. Security auditor: Low (the planner can make up to 60 steps, read-only, after a short plan; every step is a Hub quota reservation). Suggest rewording ADR §4 to «while the request has an approved plan».
- **`RevisePlan` / `ApprovePlan` keep the «second write needs a plan» rule**: plan steps are no longer in `Writes`, so when a plan step ran and `Writes == 0`, `Writes` becomes 1 (helper `keepPlanWrite`, at «Изменить» and before the next «Да» resets the used steps). Fits `valid()`, no storage change. After «Изменить» the request is back to 8 steps / `MaxInputTokens`; a request that already ran more than 8 steps stops at its next step (tested).
- ADR/task text says `MaxInputTokens` 60 000; the code has `MaxSteps*MaxStepInputTokens` = 160 000 since PR #327 — kept. The ADR/task wording should say 160 000.
- Files outside the task list, needed: `input/agent/driver.go` and `driver_test.go`, `cases/agentturn/service.go`, `desktop/tests/agent_eval_test.go`.

## Follow-ups (Hub side, not changed; the plan does not actually run long until they land)
- Hub caps every call's arguments at 8 KiB (`hub/.../values/agent.go` `MaxArgumentsBytes`), so `show_plan` over 8 KiB (about 100+ steps) is refused before the desktop sees it; the schema still says 1–8 steps — TASK-325 (catalog 10).
- Hub `MaxAgentTurns = 64`, while the desktop sends the current request whole (1 + 2×steps turns): a request past about 31 planner steps exceeds it. The desktop fake Hub does not enforce it. Needs a Hub change (raise the limit for plan runs, or trim old steps of the request) — add to TASK-325 or a new Hub task.
- Planner output is capped at 4096 tokens (`agentturn.MaxOutputTokens` and the Hub alias): a plan of many steps may not fit in one `show_plan` reply.
- TASK-245 (also in `domain/agent/thread`, origin rule) was declared sequential to this task; it still has to be rebased on this change.

## Changed existing tests
- `thread_test.go`: `TestBudget/writes` -> `unplanned writes` (a plan's 7th write is now `ErrNotInPlan`; the `MaxWrites` stop is tested by restoring `Writes = MaxWrites` without a plan); `TestApprovedPlan` Writes 2 -> 0; `TestProposePlanRules` «too many writes» now valid; `TestRestore` «steps over budget» uses `MaxPlanRunSteps + 1`.
- `paidwork_test.go`: «paid steps are capped by the write limit» -> «not capped» (7 paid drafts run, budget empty); «each listed draft once» Writes 5 -> 0. `freework_test.go`: a plan step expects 0 writes. `driver_test.go`: `TestDriverLoopIsBounded` expects `MaxPlanRunSteps`.
- `desktop/tests/agent_eval_test.go`: «plan with too many writes» (expected a refusal) replaced by a 30-step plan run end to end.

## Security
Auditor: no High/Medium; one Low (the plan limits after the plan is used up, above). Reviewer: one Medium (a second «Да» erased the record that plan steps ran, reopening an unplanned first write after «Изменить») — fixed with `keepPlanWrite` and a test; lows recorded here. Hard rule 3 holds: a write under an approved plan runs only if it matches an unused approved step.
