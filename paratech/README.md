# Learning Assistant (code)

Local-first Windows assistant for online classes. This repository contains only the code:

- `desktop/` — Windows application (Wails v3 shell, Go core, React frontend)
- `hub/` — cloud LLM Hub (Connect-RPC gateway, quotas)
- `shared/` — protobuf contracts and generated code
- `go.work` — Go workspace tying the modules together

## Setup
Required tools: Git, Go 1.26.9+, Node.js LTS (frontend), `golangci-lint` v2, `buf`. Windows builds also need WebView2 (bundled with recent Windows).

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

Contract compatibility (`buf breaking`, rule set FILE from `shared/buf.yaml`). The git repository root is the parent of this directory, so
the input must point at the `.git` directory above and at the module's subdirectory; run it from `paratech/shared`:

```bash
# against the previous commit (any ref works: HEAD~1, master, a tag)
(cd shared && buf breaking --against '../../.git#ref=HEAD~1,subdir=paratech/shared')
```

Exit code 0 means no breaking change; 100 means breaking changes were found and are listed. Use `ref=`: the form `.git#branch=HEAD~1` does not work in this
layout (`HEAD~1` is not a branch, and `.git` is not under `shared/`). When `paratech/` becomes its own repository the module moves to `shared/` of that
repository and the command becomes `(cd shared && buf breaking --against '../.git#ref=HEAD~1,subdir=shared')` (not verified yet). It is not part of CI
yet, see ADR-0005.

## Local database (SQLite adapter)
`desktop/internal/adapters/sqlite` owns the database: driver `modernc.org/sqlite` (pure Go, the desktop builds with `CGO_ENABLED=0`),
goose migrations that live in `desktop/pkg/migrations` (ARCH-04, ADR-0006: migrations of any database always live in `pkg/migrations`; the adapter only runs them), and sqlc queries in `queries/` with generated code in `sqlcgen/`
(committed, never edited by hand). `docs/contracts/schema.sql` is the source of truth for the schema; a test fails when the migrations and that file
define different objects. A schema change is a NEW migration file in `desktop/pkg/migrations` (never edit an applied one) plus the same change in `docs/contracts/schema.sql`.

Regenerate sqlc code after changing `desktop/pkg/migrations/` or `queries/` (from this directory; sqlc is run from the Go module proxy, it is not a dependency of go.mod;
the version is pinned here and recorded in `docs/refs/sqlite-goose-sqlc.md`):

```bash
CGO_ENABLED=0 go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate -f desktop/internal/adapters/sqlite/sqlc.yaml
git diff --stat desktop/internal/adapters/sqlite/sqlcgen   # commit the result
```

The first run downloads and compiles sqlc and takes a few minutes; later runs use the Go build cache.

## Hub API keys
Every Hub RPC needs a per-tester key in `Authorization: Bearer <key>`; the Hub stores only a SHA-256 of the secret. Manage keys with the hub
binary (needs `HUB_DATABASE_URL`, applies pending migrations first):

```bash
go run ./hub/cmd/hub keys create -label tester-07     # prints the key ONCE to stdout; it cannot be shown again
go run ./hub/cmd/hub keys list                        # ids, labels, status; never secrets
go run ./hub/cmd/hub keys revoke -id <id>             # rejected from the next request on
```

## Hub quota
Each tester (API key) has one allowance that does not renew; clients read it with `QuotaService.GetRemaining`. Reserve, Settle
and Release are Hub-internal (ADR-0005(f), ADR-0008). Settings (env or flag):

| Env | Flag | Default | Rule |
|---|---|---|---|
| `HUB_QUOTA_UNIT` | `-quota-unit` | `tokens` | only `tokens` |
| `HUB_QUOTA_LIMIT` | `-quota-limit` | `1000000` | > 0; a new value applies to every tester at once |
| `HUB_QUOTA_RESERVATION_TTL` | `-quota-reservation-ttl` | `10m` | 1m..1h, longer than the longest gateway request |
| `HUB_IDEMPOTENCY_RETENTION` | `-idempotency-retention` | `24h` | 1h..24h |

Code architecture rules are enforced by the root `.golangci.yml` (depguard) and `tests/arch_test.go` in each module.
