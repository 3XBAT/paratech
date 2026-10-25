# TASK-140: «Стоп» during a running agent step (core)

Status: done. All acceptance criteria hold.

## Done
- `agentturn.Progress{Started(ctx, Step)}`: optional port in `Deps`; `drive` calls it after the open step is saved and the history is read, right before `AgentModel.Step`, with the request state and no cards. `input/agent/progress.go` implements it over `Sink` (`Update{Progress: true}`); `app/agent.go` builds the panel first and gives `Progress` the panel sink (no Driver <-> Service cycle). The Wails update view has `"progress"`; `bindings/agent.ts` parses it.
- Driver «Стоп»: a per-(thread, turn) mark is set before the operation starts. The loop checks it before each call and after the last one (so no `Submit`, no next Hub step). Read (R) in flight is cut by its own context and gets no outcome ("not done"); write (U) and proposal (C) run under `WithoutCancel`, finish and keep their journaled outcome; then `Interrupt` keeps the stored reason `user`. A new «Стоп» drops older marks of its thread (bounded memory).
- Hub step in flight is not cancelled; «Продолжить» replays its key. «Стоп» never cancels a paid job.
- Review fix (medium): «Продолжить»/«Повторить»/approve/revise/answer pressed while the cut loop of the thread still runs is refused with `ProblemBusy` (`callsRunning`); the loop's in-flight entry is released only after `Interrupt`/`Submit`; a cut loop always ends through `Interrupt`. Test `TestContinueDuringAnActionCutByStop`. Without it the request could stay EXECUTING with an unanswered read.
- Supervisor follow-up: after `abort` and a non-stopped version conflict the case returns the STORED request with the error, and the Driver publishes it.

## Owner bug 2026-10-24 (answer to the previous question after «нет связи» and two «Повторить»)
No desktop fault found. Every attempt of the new request (first try, «Продолжить», «Повторить» r1/r2) sends the same history `[text "что такое горутина", text "кто такой рыбаков"]` under keys unique to turn 2 (pinned by `TestAttemptsOfANewRequestEndWithItsText`). Mechanism:
1. `domain/agent/thread/budget.go:45-67` (`Window`) sends earlier requests as bare student texts, no echo, no reply (ADR-0023).
2. `hub/internal/adapters/anthropic/wire_agent.go:115-124` (`agentMessages.add`) merges consecutive user texts into ONE user message with both questions and nothing showing the first is answered.
3. `hub/pkg/prompts/agent/panel/v2/system.md` does not say that only the last text is the current request.
Each «Повторить» is a new paid sample under a new key, so each is another chance for the model to answer the first question. Not the cause: the answer reader key (`answer.NewRequest` hashes question+fragments, so a different question has a different key; TASK-142 does not fix this), the frontend (`agentState.ts:97-99` `pendingLocal` could attach an update for an unknown turn, but nothing dispatches the old turn). Needs a decision/task outside this one: (a) `thread.Window` marks earlier texts as earlier requests or includes their replies; or (b) Hub prompt/wire put earlier texts in a block saying they are past, the last text is the request (touches ADR-0023).

## Decisions to confirm
- Resume operations are refused as busy (not queued) while a stopped loop of the thread still finishes a write.

## Follow-ups
- TASK-143: the frontend replaces an entry's update with the card-less progress update, so cards are hidden until the next update; ignore a progress update for a (thread, turn) already shown stopped (a progress update can arrive after STOPPED); the busy text can stay under the stopped card if it arrives after the Interrupt update.
- Low: short window between a step handed over and its calls registered in `inflight` (not a regression); `release` is not deferred (nothing recovers panics today).
- A paid read in flight (answer_question, inspect_upload) is cut on «Стоп» like any read; asking again is free if the Hub already answered.

## Changed existing tests
- `TestAgentPanelEventContract`: expected JSON gains `"progress":false`. Test fakes gained hooks/counters only.

## Security
security-auditor: no findings. Reviewer: one medium (fixed, re-reviewed, approve), lows listed above.
