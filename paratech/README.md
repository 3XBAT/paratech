# Learning Assistant (code)

Local-first Windows assistant for online classes. This repository contains only the code:

- `desktop/` — Windows application (Wails v3 shell, Go core, React frontend)
- `hub/` — cloud LLM Hub (Connect-RPC gateway, quotas)
- `shared/` — protobuf contracts and generated code
- `go.work` — Go workspace tying the modules together

## Setup
Required tools: Git, Go 1.26+, Node.js LTS (frontend), `golangci-lint` v2, `buf`. Windows builds also need WebView2 (bundled with recent Windows).

Windows (PowerShell or Git Bash):
```bash
winget install --source winget --id Git.Git
winget install --source winget --id GoLang.Go
winget install --source winget --id OpenJS.NodeJS.LTS
winget install --source winget --id GolangCI.golangci-lint
winget install --source winget --id bufbuild.buf
```

macOS (Homebrew):
```bash
brew install git go node golangci-lint bufbuild/buf/buf
```

Open a new terminal afterwards so the updated `PATH` is picked up, then check `go version`, `golangci-lint --version` and `buf --version`.
Run all commands below from this directory (`paratech/`).

## Build
On Windows run these in Git Bash (they use POSIX shell syntax).

```bash
go build ./desktop/... ./hub/... ./shared/...
go vet ./desktop/... ./hub/... ./shared/...
go test -race ./desktop/... ./hub/... ./shared/...
GOOS=windows go vet ./desktop/...   # check Windows-only code on any OS
golangci-lint run ./desktop/... ./hub/... ./shared/...   # bare ./... fails: paratech/ is a workspace root, not a module
(cd shared && buf lint && buf generate)   # regenerates shared/gen from shared/proto
```

Code architecture rules are enforced by `.golangci.yml` and `tests/arch_test.go` in each module.
