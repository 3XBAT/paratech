# input/agent

The loop of the agent panel (ADR-0021): `Driver` runs a request step by step on
`paratech/desktop/internal/cases/agentturn`, runs the tool calls a step admits through narrow interfaces on the use
cases (ARCH-42) and publishes updates and cards to a `Sink`. It decides nothing (class, plan, provenance and budget
come from `agentturn` and `paratech/desktop/internal/domain/agent`) and has no method that confirms, executes, deletes
or sends. See `doc.go`. File names below are in this package unless a path is given.

## Map
- `driver.go`: `Driver`, `Deps`, `New`, the request operations (`Ask`, `ApprovePlan`, `Stop`, `Rerun`, ...), `Update`,
  `Sink`. `shown.go`: the tool cards of the newest request of each thread, merged into every later update with a
  status; `displayOrder` puts the agent's cards first, then confirm/action/schedule/delete cards, then the stop line.
  `progress.go`, `replydraft.go`: progress (`Update.Progress`; `Update.Retry` 1..3 while a failed step is retried by
  itself, ADR-0026 addendum 2026-11-08) and the streamed reply draft to the panel. `compact.go`:
  `Compactor` (cases/agentcompact), run by `Ask` of a named chat before its first step; publishes the
  `Compaction`/`Compacting` updates (ADR-0028). `driver_queue.go`: the waiting questions (ADR-0026 addendum
  2026-11-08, point 7): `Ask` queues synchronously through `agentturn.Enqueue` when the chat is busy and returns the
  ticket; `drain` starts the oldest one (`agentturn.StartQueued`) when the last loop of the chat ends; `Withdraw`.
- `tools.go`: the read-only tool dispatch (`tools.reader`, `tools.run`) and the R tools; `facts.go`: `Facts` for
  `agentturn.CallFacts`.
- `write.go` + `actions.go`: class U. `write.go` journals PENDING -> DONE/FAILED and replays a call seen before;
  `actions.go` (`Actions`, `ActionDeps`, `Actions.prepare`) runs the action and its Undo (`Check`, `Apply`,
  `compensate`, `Observe`). `lines.go`: the wording of the action line.
- `propose.go`: class C, a proposal or preview plus a confirm card only. `offer.go`: class UI (`cardTool`).
- One file per tool group: `assignment*.go`, `notes*.go`, `settings.go`, `recording.go`, `quota.go`, `subjects.go`,
  `answer.go`, `turnkey.go`, `reply.go`.

## Where to add a tool
Order per ADR-0033 §5 and §7 (tools ship in catalog waves, never one catalog per tool):
1. `paratech/desktop/internal/domain/agent/values`: the tool and its arguments
   (`paratech/desktop/internal/domain/agent/values/tool.go`, or a wave file such as
   `paratech/desktop/internal/domain/agent/values/wave10.go`).
2. `paratech/desktop/internal/domain/agent/services/actionpolicy/policy.go`: its class and rules.
3. Here, a file for the group: the narrow interface on the use case, the handler, then the dispatch by class:
   R in `tools.reader` (`tools.go`, falls back to `controlReader`), U in `Actions.prepare` plus its compensation (`actions.go`), C in `canPropose`
   and `makeProposal` (`propose.go`), UI in `cardTool` (`offer.go`). A new dependency is a field of `Deps`
   (`driver.go`) or `ActionDeps` (`actions.go`).
4. `paratech/desktop/internal/app/agent.go`: add the use case to `agentReads` (filled in `paratech/desktop/internal/app/core.go`) and to the
   `Deps`/`ActionDeps` literal in `wireAgent`.
5. Catalog: `paratech/hub/pkg/prompts/agent/panel/v<N>/tools.json` equals the golden
   `paratech/shared/testdata/tool_catalog_v<N>.json`; the Hub ships first, then `values.CatalogVersion`.
A C tool's «Да» never runs here: the card is rendered by `AgentPanel` and confirmed by `AgentConfirmService`
(`paratech/desktop/internal/input/wails/agentconfirm*.go`); see the README of `paratech/desktop/internal/input/wails`.

## Rules
ARCH-40..42 and §6 of `docs/architecture.md` (ARCH-20 is why the loop is `input`, not a use case). ADR-0021 (loop,
classes, journal, the accepted ARCH-23/41 deviation of `Actions`), ADR-0022 (untrusted input: results carry IDs,
enums and counters, never error texts), ADR-0026 (stop, rerun, streaming), ADR-0028 (chats), ADR-0033 (parity,
classes, waves).

## Tests
`go -C paratech test -race ./desktop/internal/input/agent/...` (no Windows-only code here). Fakes: `fakes_test.go`
(`reads`, `scriptedTurns` with its queue hooks, `noQueue` for a Turns fake that never queues, `chanSink`,
`fakeLibrary`), `driver_queue_test.go` (`stateStore`, `stepModel`: the real `agentturn` under the Driver),
`compact_test.go` (`fakeCompactor`), `actions_fakes_test.go` (`world`, `memLibrary`, `memActions`,
`directTx`), `notes_fakes_test.go` (`notesWorld`), `assignment_wave10_fakes_test.go` (`memChanges`,
`newWave10World`). Scenario tests on real use cases and SQLite: `paratech/desktop/tests/agent_*_test.go`; evals:
`paratech/desktop/tests/agenteval`.

## Gotchas
- The panel replaces the cards of a request with those of its latest update. So tool cards (answers, confirm cards,
  action lines, buttons) accumulate per request in the Driver (`shown.go`, deduped, capped) and ride on every later
  update; step cards (plan, questions, reply, stopped, cannot_do) do not. Progress, Compaction, failed-Ask and
  problem-only updates (no status) carry none.
- `TestAgentHasNoPathToIrreversibleActions` (`propose_test.go`) fails on an interface method whose name contains
  `Confirm`, `Execute`, `Send`, `Decline`, `Delete` or `MarkSent`, and on calls of confirm or side-effect methods. A
  soft delete is therefore `TrashAssignment`, mapped by `agentAssignments` in
  `paratech/desktop/internal/app/agent.go` (TASK-320).
- The queue lives in `agentturn`; the Driver only triggers it. Every loop drains its chat AFTER `forget` (no longer
  among the loops), and a queued `Ask` drains too, so a question queued while the last loop ends is never missed. A
  loop that has not named its chat (`""`, an Ask of the current chat) holds drains back until any loop ends
  (`later`). A drain that started nothing drains again only when a drain of its own chat was deferred to it meanwhile
  (`deferred`); ends of other chats' loops never wake it (no spin). A drain compacts the chat only when
  `agentturn.QueueReady` says the question can start. After `Close` nothing drains: waiting questions are never sent.
  A new `Turns` method needs a stub in every Turns fake (`noQueue`).
- A drain that fails to read the store ends idle and does not retry; the question waits until another loop of that
  chat ends or it is withdrawn.
- A tool this desktop does not dispatch answers `values.RefuseUnavailable`, so a missing dispatch branch fails quietly.
- `values.CatalogVersion` is still 9: the v10 tools (`paratech/desktop/internal/domain/agent/values/wave10*.go`) are
  declared and dispatched here but do not parse yet; tests such as `TestNoteToolsAreNotServedAtCatalog9` pin that
  until the switch task (TASK-326).

## E2E
The planned agent pack of ADR-0035 §6 (`paratech/e2e/specs/<pack>`, the directory does not exist yet) covers this
package.
