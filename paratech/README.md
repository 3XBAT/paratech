# Learning Assistant (code)

Local-first Windows assistant for online classes. This repository contains only the code:

- `desktop/` — Windows application (Wails v3 shell, Go core, React frontend)
- `hub/` — cloud LLM Hub (Connect-RPC gateway, quotas)
- `shared/` — protobuf contracts and generated code
- `go.work` — Go workspace tying the modules together

## Build
Requirements: Go 1.26+, Node.js LTS (frontend). Windows builds also need WebView2 (bundled with recent Windows).

```bash
go build ./desktop/... ./hub/... ./shared/...
go vet ./desktop/... ./hub/... ./shared/...
go test -race ./desktop/... ./hub/... ./shared/...
GOOS=windows go vet ./desktop/...   # check Windows-only code on any OS
```

Code architecture rules are enforced by `.golangci.yml` and `tests/arch_test.go` in each module.
