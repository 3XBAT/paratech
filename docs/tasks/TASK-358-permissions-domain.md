# TASK-358: Permissions — domain, migration and storage

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** — (follows ADR-0034 В3 = а, one permission for all subjects; rewritten if the owner picks б)
- **PRD sections / AC:** ADR-0034 §5; PRD AC-14, §13.3; ARCH-11, 14, 17, 19, 62
- **Агент:** только UI: control of the agent itself (permissions are never a tool and never in `set_setting`)
- **Branch:** ralph/<date>-task-358

## Goal
The core can store, per kind of external send, whether the student is asked, and whether the first-time
«Больше не спрашивать» was already offered; a proposal remembers it was confirmed by a permission.

## Allowed to change
- `paratech/desktop/internal/domain/settings/settings/*`
- new `paratech/desktop/internal/domain/coursework/services/autoconfirm/*`
- `paratech/desktop/internal/domain/coursework/proposal/*` (ByPermission only)
- `paratech/desktop/internal/adapters/sqlite/{settings*,proposals*}.go`, `adapters/sqlite/queries/*`, `adapters/sqlite/sqlcgen/*`
- `paratech/desktop/pkg/migrations/*` (one new file), `docs/contracts/schema.sql`, tests

## Out of scope
- Use cases, bindings, UI (TASK-359…361).

## Acceptance criteria
- [ ] `settings`: `PermissionKind` (`send_to_teacher`, `deadline_to_calendar`; closed), `PermissionMode` (`ask`
      default, `allow`), `Permission(kind) {Mode, Offered}`, `SetPermission(kind, mode, now)`, `MarkPermissionOffered(kind, now)`;
      unknown kind/mode → domain error; a change bumps the version like the other setters; table tests.
- [ ] `coursework/services/autoconfirm` (pure, own mode enum): `Decide(mode, proposalKind, needsChoice) → Ask | Confirm`:
      Confirm only for `allow` and kinds send_homework_email / send_work_email (send_to_teacher) or create_deadline_event
      (deadline_to_calendar) and `needsChoice = false`; `export_homework` and everything else → Ask; table test of all cases.
- [ ] `proposal`: confirming records `ByPermission bool` (a new confirm method or option; the state machine unchanged;
      Restore reads it); test of both paths.
- [ ] Migration (next free number at push time; not in parallel with another migration task): settings columns
      `permission_send_to_teacher`, `permission_deadline_to_calendar` TEXT NOT NULL DEFAULT 'ask' CHECK (IN ('ask','allow')),
      `*_offered` INTEGER NOT NULL DEFAULT 0; proposals `confirmed_by_permission` INTEGER NOT NULL DEFAULT 0; old DBs open;
      `docs/contracts/schema.sql` mirrors it.
- [ ] sqlite roundtrip tests; migrate_test and schema_test green; `sqlc generate` without diff.
- [ ] `go test -race` of the touched packages, the architecture test, `golangci-lint run ./desktop/...` pass.
