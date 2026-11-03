# TASK-323 — wave 10 on the desktop: «Завести предметы из расписания» card and offer_button; not served until TASK-326

## Done
- `input/agent/subjects.go`: `propose_subjects_from_schedule{}` (C). Builds `ScheduleSubjectsCard` from `cases/library.ScheduleSubjects` (the page's use case). The journal records the call PROPOSED with target `proposal:<token>` (64 hex is a valid Ref); group refs are `schedule_group:g-<32 hex of sha256(key)>` (keys are schedule text, never IDs). The planner gets `{"groups":N,"proposed":true,"waits_for":"student"}`; an empty proposal is `{"nothing_to_create":true}` with no card and no entry; a repeated call ID is answered from the journal. Tests: the result JSON holds no token, key, group/subject ID or schedule string. The agent port has one method (`ScheduleSubjects`); `TestAgentCannotCreateSubjects` (AST) finds no `CreateFromSchedule`/`UndoFromSchedule` in the package.
- `input/wails/agentconfirm_schedule.go`: `ConfirmSchedule(token, choices)` («Да», through the page's `LibraryService.CreateFromSchedule`; a doubtful member without a choice stays merged, О-12 б), `UndoSchedule(token)` («Отменить всё»), `Card("schedule_subjects", token)`, `Decline`. States waiting / confirmed (partial, «Да» again is safe) / done / undone / stale / declined. Rows skipped because a removed subject holds the name (also a rename colliding with one, ErrRemovedNameTaken) carry `RemovedSubjectID`/`Version` for «Вернуть» (the page's `RestoreSubject`).
- `offer_button` (`input/agent/offer.go`, `input/wails/agentconfirm_offer.go`, `agentpanel.go` `cardView`): card from the action and its one ID only; extra args refused, ID checked by `thread.Admit` provenance (`unknown_id`), every action of the closed enum tested; `open_call_link`/`copy_call_link` answer `unavailable` (TASK-261 not in; TASK-328); not journaled, not a write; the planner gets `{"shown":true}`. Panel view `AgentOfferView{action,target}` has no URL/path/label; `AgentCardView.Offer` is `omitempty` so existing event bytes are unchanged.
- Wiring: `Driver.Deps.Subjects`, `app/agent.go`; the cards are bound to the page's single `*LibraryService` through the package function `BindScheduleCards` in `shell.go` (a function, not a method, so the page cannot call it).
- `CatalogVersion` stays 9; no Hub change; no new dependency.

## Decisions to confirm
1. Created subjects reach the planner through the next step's `AgentBase.Subjects` (rebuilt every step, so IDs resolve by provenance); a schedule name enters the base only after the student's «Да».
2. Result of «Да»/«Отменить всё» lives in the page binding's in-memory store (as TASK-312): after an app restart an applied card reads `stale` and undo answers that it cannot be done here, pointing to «Предметы». Persisting the outcome would put schedule text in the agent journal: needs a sqlite codec and a decision (Medium follow-up).
3. After a partial «Да» the choices are kept per token (in memory) and merged into the next «Да» (choices sent now win), so a skipped or renamed group is never created by the defaults.
4. `values/wave10schedule.go` (result shapes) and the `Driver`/`propose.go`/`tools.go` hooks are outside the listed files; `shell.go` changed for the single shared binding (approved by the coordinator).
5. Panel buttons: TASK-324 maps each `offer_button` action to the page's button/handler (many are navigation, not one binding call) and binds `ConfirmSchedule`/`UndoSchedule`/`RestoreSubject`. The tool text for the v10 catalog (TASK-325) must document the result shapes above.

## Follow-ups
- Medium: persist the card outcome (see 2).
- The `delete_assignment` gap in `cardView` (TASK-320) is still open.
- Low: a card whose «Да» stopped midway keeps showing `confirmed` if the page then finishes the batch; the page's undo is not reflected on the card (shows `done`); the in-memory maps (`partial`/`undone`/`declined`) have no cap; a «Да» that only skipped rows stores no result, so a re-read shows waiting.
- `docs/agent-parity.md` unchanged (dispatch tasks update it when served).

## Changed existing tests
`TestScheduleCardStale`, `TestScheduleUndoProblems`, `TestScheduleDecline`, `TestScheduleConfirmRefusals`, the helper `newScheduleConfirm` (all new in this task, changed after review: wording and the constructor). `app/agentschedule_test.go` checks the shared page binding. No test of an earlier task was changed or weakened.

## Security
Auditor: no findings (no create/undo path in `input/agent`; token forgery gains nothing, the use case rebuilds the proposal; no schedule text in planner results, logs or journal; offer_button forges nothing). Reviewer: first round three Mediums (a stale message claiming nothing was created; choices lost on a second «Да»; separate library binding) and three Lows, all fixed in 89eb31eb; re-review: approve, three Lows recorded above.
