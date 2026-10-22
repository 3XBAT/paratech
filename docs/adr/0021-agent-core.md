# ADR-0021: Agent as the core

- **Status:** Accepted
- **Date:** 2026-10-22
- **Source:** TASK-112; owner's decision of 2026-10-21 (mockup `docs/design/concept-agent.html`); design
  `paratech-ops/research/agent-core-design-2026-10.md` §2 (outside the repo); ADR-0004, ADR-0017, ADR-0018, ADR-0022–0024.

## Context
The owner wants an agent panel over the whole app (Ctrl+K, «Спросить paratech», drop a file anywhere). A cheap planner
model picks tools; heavy work stays in the existing jobs with a strong model. The answers are typed cards in an
impersonal voice («План: …», never «я»). Tools are the existing use cases, so the agent is one more client of them.

## Decision
- **Loop in `input/agent`** (new transport, ARCH-01): `Driver` runs a request step by step and dispatches tool calls to
  use cases through its own narrow interfaces (ARCH-42). Only `input` may call several use cases (ARCH-20). The Driver
  makes no decisions: class, plan, provenance and budget come from `cases/agentturn` and the domain (ARCH-41).
- **Step in `cases/agentturn`**: Hub call through port `AgentModel` (`adapters/hubclient`), argument check, policy,
  budget, thread. Journal in `cases/agentjournal`. Step key `ag:<thread>:<turn>:<step>`.
- **`input/wails` → `input/agent` without import**: `input/wails` declares an interface `Agent`, `app` plugs in
  `agent.Driver`. Only domain types and primitives cross it. Allowed pattern, not an exception to §9.
- **Context `agent`**: aggregates `thread` and `action`, `values` (Tool, argument parsing, Card, Scope),
  `services/actionpolicy`.
- **New IDs** (SubjectID, MaterialID, FolderID, ThreadID, ActionID) live in `domain/shared`; the result mapper
  (ADR-0022) takes agent-owned field structs (primitives + shared IDs) that the case/Driver fills from other contexts'
  views, so `domain/agent` imports no other context.
- **Threads per subject**: key = `SubjectID` or `NONE` («Без предмета»); the context chip picks the thread. A step
  carries the shared base (profile, subject list, today, tz), the current subject scope and the last turns of **this**
  thread only (≤ 12 turns or 24 KB; older turns shrink to action lines). Kept 90 days, then only journal lines.
- **Tool classes** (`actionpolicy`, a tool outside the table is an error): **R** read-only, free; **U** reversible
  local, done at once with «Отменить»; **C** irreversible, creates a proposal/preview and a confirm card; **UI** card
  only (`show_plan`, `ask_questions`, `reply`, `cannot_do`).
- **Class U criterion**: a tool is U only if it changes local SQLite or the private artifact store, or starts a paid
  Hub job, **and** has a typed compensation. Anything touching the network beyond the Hub, user-visible files,
  calendar, email or deletion is C. Moving a tool to U needs a review.
- **C is never executed by the agent**: there is no method for it. `Confirm/Execute` is called only by `input/wails`
  on the student's click.
- **Plan first** (`NeedsPlan`, counted over the whole request, not per step) when: a second write in the request; any
  paid job above ≈ $0.05; replacing a ready result (class C, see below); triage says «partly/impossible». Lecture text, turnkey
  work and batches always go through a plan. Else one U action runs at once.
- **Plan card**: planned calls are structured steps (tool + argument IDs) rendered by desktop templates; model text is
  only a caption. «Да» is a click in `input/wails` that records the approved step list in the thread. Every later write
  in the request must match an approved step (tool and IDs), else `is_error`; it is never executed. Each approved
  step runs at most once.
- **Replacing a ready result** stays U but always needs an approved plan (TASK-123); its Undo can only stop the job
  while it runs, the old result is not kept.
- **Provenance** (typed): an ID in arguments passes only if it appeared in a field of the same ID type in the
  scope/base or in tool results of this thread, else `is_error`.
- **Budget per request** (`thread`): ≤ 8 steps, ≤ 4 calls per step, ≤ 6 writes, ≤ 1 paid job without a plan,
  ≤ 60k planner input tokens; ≤ 2 question rounds, a third is refused. Over it: card «Остановлено: лимит просьбы».
- **Journal and Undo** (`agent/action`): `PENDING → DONE | FAILED`, `DONE → UNDOING → UNDONE | UNDO_REFUSED`,
  `PROPOSED` for C (status read from the proposal). Two short transactions (record, then outcome); the use case between
  them is idempotent by a key namespaced by thread and action, not `call_id` alone (ARCH-23, no new ADR-0004
  exception). Compensation is idempotent and checks the target version in the same transaction as the change:
  changed since → `UNDO_REFUSED` (reason `changed`, `result_ready` or `cannot_stop`); already compensated → `UNDONE`.
  PENDING stores an intent (target, its version before, prior state); at start it is settled: effect found → `DONE`,
  target unchanged → `FAILED`, else `UNKNOWN` (paid jobs always). `UNDOING` is retried. A call ID runs once per request.
- **Compensations**: subject/folder → removed if empty; placed upload → back to «Входящие» (file kept); link →
  unlinked; lesson info / due date → old values stored in the record (D7); paid job running → `jobrunner.Cancel`,
  finished → refused (`result_ready`, no previous version kept).
- **Cards** (`agent/values`): Plan, Questions (round 1..2), Progress (job), Result, Confirm (proposal/preview only),
  ActionLine (Undo), Answer (verified citations), CannotDo. Texts are desktop templates; model text only in
  `show_plan/reply/cannot_do` fields, ≤ 300 characters, no HTML.
- **Posts after a lesson**: `cases/todayfeed`, deterministic, no LLM, wired by `app` to `jobrunner` hooks on
  post-processing events (no case → case import, ARCH-20); «Да, в календарь» is the existing `create_deadline_event`
  proposal.
- **Offline**: input disabled («Нет связи с paratech»), threads shown «нет связи — может быть неактуально», card
  buttons work. A failed step leaves the request `STOPPED`; «Продолжить» repeats the same key (free replay).

## Hard rule 3 and ARCH-27
A reversible local U action with a journal entry and «Отменить» is the «policy» step of «LLM proposes → Core validates
→ policy/user confirmation → adapter acts». External and destructive effects need the user's click. CLAUDE.md wording
is unchanged.

## Consequences
- Money spent on a paid U action is not undone; the budget and the $0.05 plan threshold bound it.
- Desktop migration (next free number): `agent_threads`, `agent_turns`, `agent_steps`, `agent_actions`.
- Policy tests are CI-blocking (ADR-0022): C never runs, ID without typed provenance, write outside the approved plan,
  3rd question round, budget, and `input/agent` interfaces expose no `Confirm/Execute/Delete/Send` method.
- Open: keeping previous versions of a paid result (so Undo of a finished replacement can restore it); where they
  are kept is decided in the task that builds it.
- `input/agent.Actions` implements the journal's Compensator and Resolver ports over the same use cases (app wires it).
