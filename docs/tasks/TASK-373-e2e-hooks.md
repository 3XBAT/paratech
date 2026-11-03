# TASK-373: E2E hooks: file pickers, save dialogs, opener and clipboard

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-372
- **PRD sections / AC:** ADR-0035 §3; ARCH-33, ARCH-40, ARCH-50; hard rule 5
- **Агент:** нет: тестовая инфраструктура
- **Branch:** ralph/<date>-task-373

## Goal
In E2E mode the runner answers every native dialog through a file and sees every link, file opening and clipboard
write instead of them really happening; outside E2E mode nothing changes.

## Allowed to change
- `paratech/desktop/internal/adapters/e2ehooks/**` (new adapter, std only)
- `paratech/desktop/internal/input/wails/{shell.go,shell_windows.go,shell_other.go}`, `*picker_windows.go` only to
  route through the optional overrides, tests
- `paratech/desktop/internal/app/e2e.go` and tests
- `paratech/desktop/tests/arch_test.go` only if the new adapter needs an entry

## Out of scope
- Any change of behaviour without E2E mode; the runner side (TASK-375).

## Acceptance criteria
- [ ] `wails.Options` gets optional overrides (open pickers, save pickers, link/file opener, clipboard); nil = today's
      native behaviour. `input/wails` does not import `adapters` (ARCH-40); `app` wires `e2ehooks` only in E2E mode.
- [ ] Open dialogs (schedule, model, material, notes, recording import) take the next line of `<e2e-dir>/picks.jsonl`
      (`{"dialog":"recording","paths":["fixtures/clip.wav"]}`, consumed lines are recorded as consumed). A missing
      line, another `dialog`, an absolute path, `..`, a reparse point or a path outside `<e2e-dir>/fixtures` is
      «cancelled» and an error line in the log. Table tests with the traversal cases.
- [ ] Save dialogs return `<e2e-dir>/saved/<proposed file name>` (name sanitized; existing file replaced).
- [ ] `weblink`, the file opener («Открыть в Word»…) and the clipboard append `{"kind","value","at"}` to
      `<e2e-dir>/opened.jsonl` and do not act; tests check nothing is started.
- [ ] Security review of `docs/process/security-review.md` (path confinement) is part of the review.
- [ ] `go vet`, `go test -race` (with and without `-tags e2e`) of the touched packages, `GOOS=windows go vet -tags e2e
      ./desktop/...`, golangci-lint v2.14.0 pass.

## Notes
The hooks are compiled always but reachable only through E2E mode, which exists only with tag `e2e` (TASK-372).
