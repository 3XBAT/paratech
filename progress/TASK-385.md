# TASK-385: beta build with tag `beta` and hub.exe

## Done
- `paratech/desktop/build-alpha.ps1 -Beta`: tags `production,beta`, default folder `paratech\dist\beta`; refuses `-E2E` and `-ComponentOnly`. Builds `hub.exe` next to `paratech.exe` (`go -C <module> build -trimpath ./hub/cmd/hub`, CGO_ENABLED=0, windows/amd64, env restored in `finally`) and checks `go version -m` for `CGO_ENABLED=0`.
- Without `-Beta`: a stale `hub.exe` in the out dir is removed (as `learning-assistant.exe`); the exact-tags check stays (production / production,e2e / production,beta).
- Every non-beta build runs `go tool nm` through `Test-NoBetaSymbols`: fails on `internal/cases/localhub` or `internal/adapters/hubprocess`, and also when `main.main` is absent (so empty nm output cannot pass).
- `-Beta -Smoke`: after the app smoke runs `hub.exe -h` (15 s timeout, exit 0 and `Usage of hub` required; `config.MustLoad` exits 0 on `flag.ErrHelp` before any listener or database). No Hub is started.
- CI (`.github/workflows/ci.yml`): `go vet -tags beta` and `go test -tags beta ./desktop/...` (race in `checks`, plain in `windows`), mirroring e2e.
- Docs: `docs/qa/run-alpha.md` («Сборка», «Запуск», and the intro line 4, which said the beta is an `-OutDir` build and would now be wrong), `docs/qa/tasks/TASK-385.md`.

## Verification (real builds into scratchpad out dirs; the owner's `dist\beta` untouched; `-SkipWhisper`, no cache in the worktree)
- `-Beta -Smoke`: paratech.exe 52.1 MB (`-tags=production,beta`) and hub.exe 21.2 MB (`CGO_ENABLED=0`); app smoke and `hub.exe -h` passed.
- Alpha with a planted `hub.exe`: removed; nm check 47036 symbols, none of the two packages; smoke passed. E2E build: tags `production,e2e`, nm clean.
- Refusals `-Beta -E2E`, `-Beta -ComponentOnly` throw before any build. `Test-NoBetaSymbols` proven on synthetic text (clean passes; localhub, hubprocess, empty, no `main.main` fail). Exact-tags regex 3x3 matrix. `go vet -tags beta ./desktop/internal/app/...` clean. ci.yml parses.

## Decisions to confirm
1. `main.main` required in the nm output as the "nm worked" proof.
2. run-alpha.md line 4 (intro) changed with the sections.

## Follow-ups
- **Medium (outside the task):** `paratech/desktop/build-release.ps1:252` `Get-ReleaseExeProblems` checks `Contains('-tags=production')`, which also accepts `production,beta` and `production,e2e` on the `-SkipAppBuild` path (no nm check there). Use the exact regex and the nm check. A beta exe reaches the installer only by a deliberate mistake; `project.nsi` lists files by name, so `hub.exe` is never packed.
- Once TASK-389 lands, a Hub left running after the app closes locks `dist\beta\hub.exe`: the next build fails with a bare "Access denied". Add a clear message or a QA note then.
- Low: `hub.exe -h` inherits the shell environment; the usage prints non-secret flag defaults from `HUB_*` into `smoke-hub.log` (secrets are not flags).
- Owed by TASK-387/389: arch-test rule (a file without `//go:build beta` must not import `cases/localhub` or `adapters/hubprocess`); until TASK-389 the nm check passes trivially.
- Not run: with the real whisper component (smoke ran with `-SkipWhisper`).

## Changed existing tests
None (no Go code).

## Review
reviewer (opus): approve, no blocker/high; two mediums above are follow-ups outside the allowed files / TASK-389. security-auditor (opus): no finding at high or medium; the build-release note above is the only hardening point.
