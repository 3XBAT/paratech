# TASK-360: Permissions — Wails bindings and the trigger

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-359
- **PRD sections / AC:** ADR-0034 §5; ADR-0021 (no Confirm in `input/agent`); ARCH-40…44
- **Агент:** только UI: control of the agent itself; `set_setting` must keep refusing permission keys (test)
- **Branch:** ralph/<date>-task-360

## Goal
The UI can read and change permissions, offer the checkbox, and allowed cards are confirmed without a click.

## Allowed to change
- `paratech/desktop/internal/input/wails/{agentconfirm.go,agent.go,settings.go,homework*.go}` and tests
- `paratech/desktop/internal/app/*` (wiring only)
- `paratech/desktop/frontend/src/bindings/{agentConfirm,settings}.ts`, frontend tests of bindings

## Out of scope
- Russian texts and the UI (TASK-361); `input/agent` (must not change).

## Acceptance criteria
- [ ] `SettingsService.Permissions()` → list of {kind, mode, offered}; `SetPermission(kind, mode)` (unknown → invalid).
- [ ] The confirm card read returns `offerDontAsk` (kind has a permission, mode ask, not offered, not a choose-teacher
      card) and, after an automatic confirmation, the outcome with `byPermission: true`.
- [ ] Confirm calls take `dontAskAgain` (ignored with «Нет»).
- [ ] `ConfirmByPermission` is called only here: when the forwarder of agent updates emits a new confirm card, and right
      after a binding creates a send proposal from the student's button; if the agent update path does not pass through
      `input/wails`, stop and report instead of adding a call in `input/agent`.
- [ ] The agent's `set_setting` and `get_settings` neither accept nor return permissions (test).
- [ ] Tests on fakes for each path; `go test -race ./desktop/internal/input/wails/...`, architecture test,
      `GOOS=windows go vet ./desktop/...`, `npm run test` and `npm run build` pass.
