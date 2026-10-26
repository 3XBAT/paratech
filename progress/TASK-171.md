# TASK-171: thread stuck in «Предыдущая просьба ещё выполняется»

## Root cause: NOT proven
The owner's real database and desktop.log were off limits, so the diagnosis is from code. The log fact "no agent WARN since 16:59:56" excludes every path that logs. Candidates:
1. `Driver.run` ended without an error and left the request running: step returned with status PLANNING/EXECUTING and no calls (e.g. Interrupt failed after «Стоп», then «Продолжить»: `drive` finishes a closed step with unanswered admitted calls and returns no calls). Fixed (loop closes such a request).
2. The `ctx.Err() != nil` branch of `run` returned silently on error (the only silent exit). Now closed too.
3. A loop alive but stuck in a tool that never returns (tools have no own timeout; Hub calls do). Legit busy, cut by «Стоп»; UI is TASK-143. Not fixed: no tool timeout.
4. Paths that logged but left the request running: drive errors at BeginStep, StepKey, base, save-after-step, `fail`; Submit failing in applyOutcomes/save. All fixed.
Recording-caption hypothesis (ellipsis, «·», newline): `values.NewStudentText` accepts it, the step completes (`TestRecordingCaptionAsRequest`). Not the cause.
Open question: if the beta repeats, read the stored request (status, step, calls) of that thread to confirm which path.

## Done
- cases/agentturn: every `drive` error path stops the stored running request (`abort`), panic recovery in `drive`; new `RecoverIdle` (like Recover for one thread, only if the caller says no live loop); `Interrupt` now keeps what it can (`keepOutcomes`): answered/unopen calls skipped, refused outcome -> outcome_unknown; `Submit` stays strict.
- input/agent Driver: tracks live loops; Ask/Rerun getting ErrTurnInProgress with no other live loop on the thread recover once and retry; Submit error closes the request via Interrupt (executed outcomes kept, writes never "not done"); running step without calls is closed; panics in loop/tool recovered (tool write -> outcome_unknown).

## Decisions to confirm
- Safety net assumes one app instance per database.
- Panic recovery in Driver/drive (logged ERROR with stack) — project-wide policy?

## Changed existing tests
`cases/agentturn/fakes_test.go` (subjects fake with settable error), `input/agent/fakes_test.go` (RecoverIdle, refuseOutcomes first-report only, panics switch), `stop_test.go` (stub). `TestCloseDropsRefusedOutcomes` -> `TestCloseReplacesRefusedOutcomes` (stronger). `TestInterruptAfterAnExecutedCall` last check: unknown call is no longer refused; now asserts ErrInvalidTransition. `TestHungToolStopsAndTheNextRequestPasses` synchronised and asserts live was consulted. No test weakened.

## Follow-ups
- Panic in `runCalls` outside `runCall` passes no outcomes (low).
- No timeout for hung local tools.
- Optional: move the "refused outcome -> unknown" rule into a Thread method.

## Security
Auditor: no high/medium. Recovery marks outcome_unknown, no re-execution; reviewer approved after one fix round.
