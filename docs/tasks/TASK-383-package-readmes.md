# TASK-383: Short README per large package for agents

- **Agent:** go-core (sonnet)
- **Label:** docs
- **Depends on:** none
- **PRD sections / AC:** owner's earlier approval (README per large package); docs/architecture.md; ADR-0021, ADR-0033
- **Агент:** нет: документация кода
- **Branch:** ralph/<date>-task-383

## Goal
An agent opening a large package learns in two minutes what lives where and where a new piece goes.

## Allowed to change
- `paratech/desktop/internal/input/wails/README.md`
- `paratech/desktop/internal/input/agent/README.md`
- `paratech/desktop/frontend/src/README.md`
- `paratech/desktop/frontend/src/agent/README.md`

## Out of scope
- Code changes; docs outside these four files.

## Acceptance criteria
- [ ] Each README ≤ 80 lines, English: purpose (one paragraph), file/group map (by prefix, not every file), «where to
      add a binding / tool / page / card» with the files to touch, the ARCH rules and ADRs that bind it, how its tests
      run, and one line on E2E: which `paratech/e2e/specs/<pack>` covers it (ADR-0035).
- [ ] Every path and name in them exists on master at the time (checked by a script in the scratchpad, result in
      progress); nothing restates the architecture doc beyond links.
- [ ] No code file changes; `npm run build` and `go vet ./desktop/...` still pass (sanity).
