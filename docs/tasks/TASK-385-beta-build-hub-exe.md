# TASK-385: Beta build with tag `beta` and hub.exe next to paratech.exe

- **Agent:** go-core (sonnet)
- **Label:** core
- **Depends on:** none
- **PRD sections / AC:** ADR-0036 §1; ADR-0035 §3 (tag pattern)
- **Агент:** не касается (сборка)
- **Branch:** ralph/<date>-task-385

## Goal
`build-alpha.ps1 -Beta` produces `paratech\dist\beta\paratech.exe` (tags `production,beta`) and `hub.exe`; every
other build is proven to carry neither the tag nor the local-Hub code.

## Allowed to change
- `paratech/desktop/build-alpha.ps1`
- `.github/workflows/*.yml` (only: vet and test `-tags beta` for `./desktop/...`, as for `e2e`)
- `docs/qa/run-alpha.md` (sections «Сборка» and «Запуск»: `-Beta`, hub.exe; no changelog line)

## Out of scope
- Any Go code (the tagged files come with TASK-389; the symbol check below passes trivially until then).

## Acceptance criteria
- [ ] `-Beta`: tags `production,beta`, default out dir `paratech\dist\beta`; refuses `-E2E` and `-ComponentOnly`.
- [ ] `-Beta` also runs `go -C <module> build -trimpath -o <out>\hub.exe ./hub/cmd/hub` with CGO_ENABLED=0,
      windows/amd64 (env restored after), and checks `go version -m hub.exe` shows `CGO_ENABLED=0`.
- [ ] Without `-Beta` a `hub.exe` left in the out dir by an earlier beta build is removed (as `learning-assistant.exe`).
- [ ] The exact-tags check stays (`-tags=production` / `production,e2e` / `production,beta`).
- [ ] Every non-beta build: `go tool nm <exe>` has no symbol containing `internal/cases/localhub` or
      `internal/adapters/hubprocess`; else the script fails.
- [ ] `-Beta -Smoke`: also runs `hub.exe -h` and expects exit 0; the smoke never starts a Hub (headless).
- [ ] CI vets and tests `-tags beta` for the desktop module.
- [ ] Owner check `docs/qa/tasks/TASK-385.md`: `build-alpha.ps1 -Beta -Smoke` → both exes in `dist\beta`.

## Notes
Security review: the script builds and runs only files it built; nothing is downloaded for hub.exe.
</content>
</invoke>
