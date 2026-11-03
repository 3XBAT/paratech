# TASK-383 — short READMEs for large packages

Branch ralph/20261104-task-383. Docs only: four `README.md` files next to the code (the task names them; they describe code,
like `paratech/README.md`, so they live in the package folder, not in `docs/`). No code, test or config change.

## What was done
- `paratech/desktop/internal/input/wails/README.md` (62 lines): file map by prefix, ordered steps to add a binding
  (`<page>.go` -> `shell.go` -> `shell_windows.go` -> `app/core.go` -> TS mirror), test conventions, gotchas.
- `paratech/desktop/internal/input/agent/README.md` (63): map, ordered steps to add a tool by safety class (R/U/C/UI), catalog
  version rule, fakes, gotchas.
- `paratech/desktop/frontend/src/README.md` (66): pages vs `*View.ts` vs `bindings/` vs `ui/` vs `popup/`, add a page / binding,
  test runner (`test/*.test.mjs`, `tsconfig.test.json` files list).
- `paratech/desktop/frontend/src/agent/README.md` (64): panel flow, add a card kind / offer button / card button / message action.
- E2E line in each: ADR-0035 packs `paratech/e2e/specs/<pack>` do not exist on master yet (only `paratech/e2e/src`, `test`), so
  the READMEs name the planned pack and say so; `docs/process/e2e.md` is also not written yet.

## Verification (path check, scratchpad scripts check_readmes.py and verify.py)
Every backticked path and identifier was extracted and checked against the tree: 187 Go-side tokens, 0 missing after review
fixes (the one miss was a relative path I introduced and then made full); frontend: only placeholders (`bindings/x.ts`,
`XCardView`, ...) and the two paths stated as absent fail, by design. `go vet ./desktop/internal/input/...` and
`npm run build` pass.

## Review
reviewer (opus): approve, 0 blocker/high. Fixed: 2 medium (test paths now say `test/` is under `paratech/desktop/frontend`, not
`src/`; confirm-card steps now mention `cardKinds` in `bindings/agentConfirm.ts`) and lows (`agentReads` in wiring step,
`controlReader` fallback, more bindings listed). No security-auditor run: docs only, no trust boundary changed.

## Decisions to confirm
- Length: 62-66 lines each (target was 20-40, cap 80): the ordered "where to add" steps and verified gotchas take the space.
- README.md inside `paratech/` (CLAUDE.md says it holds only code); the task names these paths and READMEs describe code.
- Wails README cites ADR-0034 (navigation) too; drop if unwanted.

## Follow-ups
- `nav.ts` header comment (lines 1-2) still says the rail has a label under the icon; `ui/Rail.tsx` and `test/nav.test.mjs`
  say icon-only. `docs/design/brief.md` also describes the old rail. Outside this task.
- `docs/architecture.md` §6 calls the wails-side interface `Agent`; the code name is `AgentDriver`.
- No test covers the `RegisterService` list in `shell_windows.go` (a service missing there builds and passes, but cannot be called).
- Supervisor: suggest adding to implementer-rules.md "read the README.md of the package you change first, and update it when you
  add a file group, a registration step or a test fake".

## Changed existing tests
None.

## Security line
Docs only; nothing to audit.
