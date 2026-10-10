# T-001: Repo tooling

- **Agent:** go-core
- **Label:** core
- **Depends on:** -
- **PRD sections / AC:** §4.3
- **Branch:** task/T-001-repo-tooling

## Goal
Make the repository ready for development: lint config, `buf` config in `shared/`, working CI, documented dev commands.

## Allowed to change
- `.golangci.yml`, `shared/buf.yaml`, `shared/buf.gen.yaml`, `.github/workflows/`, `README.md`, `Makefile` or `justfile` (optional)

## Out of scope
- Any product code; real proto messages (that is T-002)

## Acceptance criteria
- [ ] `golangci-lint run ./...` configured with a sensible, small linter set and passes on the current skeleton
- [ ] `buf lint` passes on a placeholder proto; `buf generate` configured for Connect-RPC (Go)
- [ ] CI runs vet/test for all three modules; Windows job also runs `GOOS=windows go vet`
- [ ] README lists exact setup commands for Windows and macOS
