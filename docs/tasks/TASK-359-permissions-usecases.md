# TASK-359: Permissions — use cases (set, offer, confirm by permission)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-358
- **PRD sections / AC:** ADR-0034 §5; hard rules 3 and 7; ARCH-20…27
- **Агент:** только UI: control of the agent itself
- **Branch:** ralph/<date>-task-359

## Goal
A confirm card of an allowed kind is confirmed and executed by the core with exactly the facts the card would show.

## Allowed to change
- `paratech/desktop/internal/cases/preferences/*`
- `paratech/desktop/internal/cases/homeworkflow/{proposals.go,worksend.go,ports.go,types.go,errors.go}`, new
  `homeworkflow/permission.go`
- `paratech/desktop/internal/app/*` (wiring only), `paratech/desktop/tests/*`, tests

## Out of scope
- Bindings and the trigger in `input/wails` (TASK-360); UI (TASK-361).

## Acceptance criteria
- [ ] `preferences.SetPermission(kind, mode)`, `MarkPermissionOffered(kind)`, `Permissions()`.
- [ ] `homeworkflow.ConfirmByPermission(ctx, id) (ProposalView, Outcome, error)`: loads the proposal, reads the mode
      through a narrow port, `autoconfirm.Decide` (no conditions in the use case, ARCH-22); Ask → nothing changes;
      Confirm → the same confirmation as the card's «Да» with the trusted facts built now (recipient, subject line =
      ShownSubject, pinned format, deadline), `ByPermission = true`, then the normal execute path (recheck, idempotent job,
      delivery with UNKNOWN reconciliation). A second call or a proposal not VALIDATED → no-op.
- [ ] The confirm used by the card's click accepts `dontAskAgain`: with «Да» it sets the mode to `allow` (separate
      aggregate, separate transaction after the confirmation; a failure there does not undo the send and is reported);
      any answer on a card that offered the box marks `offered`.
- [ ] Scenario tests with fake SMTP and fake Google: allowed send is sent once; `ask` sends nothing; a choose-teacher send
      is never auto-confirmed; a resend after UNKNOWN is never auto-confirmed; a changed attachment is STALE, not sent.
- [ ] `go test -race` of the touched packages, the architecture test, `golangci-lint run ./desktop/...` pass.
- [ ] Security review (`docs/process/security-review.md`) of this task and TASK-360 together.
