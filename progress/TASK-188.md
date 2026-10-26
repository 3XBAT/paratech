# TASK-188 — free-work notice card for a work on a topic (agent core)

## Done
- `triage_assignment{topic}` with no approved plan and the notice on: `thread.Admit` admits it as a card only (ClassUI, nothing counted). The panel gets a `free_work` card (thread, turn, call id); the planner gets `{"status":"awaiting_student"}`. Topic is not journaled or logged.
- Notice off («Больше не показывать»): the topic triage runs at once as the single counted unplanned paid write (supervisor decision); a second paid write in the request needs a plan.
- Inside an approved plan the topic step runs directly, no card (plan approval is the consent).
- `FreeWork(ref, accept, dontShowAgain)` + read-only `FreeWorkCard(ref)` on `AgentService`. Topic and subject come from the stored call and the thread key; the page sends only the ref. Accept = journaled write through `TriageTopic` (key `ag:<thread>:<turn>:<call_id>`, sha256 hex if > 128), Undo = RemoveWork; double accept = one work, one journal entry. Decline stores nothing and calls nothing.
- Domain rule: `thread.FreeWorkCall` (pure, over this thread's history; returns call + admission from actionpolicy), `actionpolicy.FreeTopic`, `Facts.FreeWorkNotice`.
- Setting: `settings.free_work_notice` (default on), migration 0032, preferences toggle, `Settings.freeWorkNotice` / `SetFreeWorkNotice`; frontend bindings (agent.ts, settings.ts) and tests. Rendering is TASK-189.

## Scope beyond the task's file list (approved by the supervisor)
settings domain, cases/preferences, adapters/sqlite settings + sqlc + migration 0032 + docs/contracts/schema.sql, domain/agent/thread, actionpolicy; `frontend/src/agentView.ts` (`case "free_work": return null`, needed for the TS build).

## Decisions to confirm
- «Нет» stores nothing, so a reopened old thread shows a declined card as waiting again and a later «Сделать» runs once.
- Until TASK-189 renders the card, a topic request shows the student nothing (notice on by default): do not cut a tester build between this and 189.
- The work started from the card is outside the per-request write/paid budget (the card is the consent).
- No mutex in FreeWorks: a click landing while the first is still running gets `failed` (PENDING) and a later `FreeWorkCard` shows `done`.

## Follow-ups
- ADR addendum (0021/0025): free-topic rule (card «Сделать» is the consent; notice off = one unplanned paid write).
- Card lookup reads at most 1024 history rows; a card buried deeper reads as not found (refusal, fail-safe).
- After a FAILED card run, a later «Сделать» replays the failure, no retry.
- `thread.FreeWorkCall` is a stateless rule in the aggregate package (reviewer low; maybe move to a service).

## Changed existing tests
`TestTriageTopicUnavailable` -> `TestTriageTopicRuns`; prepare() calls take origin; fake turnkey gets TriageTopic; preferences fake store/defaults; migrate_test version 32; wails agent/settings tests (event contract, method count, JSON names); frontend settings/loadestimate fixtures gain `freeWorkNotice`.

## Security
Auditor: no findings. Reviewer: approve; medium (admission hard-coded in input) fixed by returning the admission from the domain; lows 2-4 fixed, others listed above.
