# paratech (code)

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
| `HUB_QUOTA_RESERVATION_TTL` | `-quota-reservation-ttl` | `10m` | 1m..1h and at least 2x `HUB_LLM_TIMEOUT` (with the default 5m timeout a TTL below 10m fails at startup; lower the timeout first) |
| `HUB_IDEMPOTENCY_RETENTION` | `-idempotency-retention` | `24h` | 1h..24h |

## Hub LLM provider
The Hub calls the Anthropic Messages API (non-streaming, std `net/http`). Clients send only a model alias; the Hub resolves it
through its allowlist and never takes a model id or URL from a client. Settings (env or flag):

| Env | Flag | Default | Rule |
|---|---|---|---|
| `HUB_ANTHROPIC_API_KEY` | none (env only) | none | required to serve (the `keys` commands do not need it); secret, never printed |
| `HUB_ANTHROPIC_BASE_URL` | `-anthropic-base-url` | `https://api.anthropic.com` | https; http only to a loopback host (local fake) |
| `HUB_LLM_TIMEOUT` | `-llm-timeout` | `5m` | 10s..10m, at most half of `HUB_QUOTA_RESERVATION_TTL` |
| `HUB_LLM_MODELS` | `-llm-models` | `default=claude-sonnet-5-5:8192,planner=claude-haiku-5-5:1024` | `alias=model-id:max_output_tokens[,...]`; config checks the syntax, the alias, model id and cap rules are checked at startup |
| `HUB_PROVIDER_STREAM` | `-provider-stream` | `false` | `CompleteStream` streams from the provider; off, it makes one unary call and sends the whole answer (ADR-0026) |

A requested `max_output_tokens` of 0 means the alias cap; a larger value is clamped to it.

## Hub LLM gateway
`GatewayService.Complete` (ADR-0010): rate limit per API key, validation (1 MiB payload), reservation of the most the call may
cost (request bytes + 1024 + the clamped output cap), provider call, then settle the reported usage and store the answer for
replay, or release. A retry with the same idempotency key replays the stored answer (`replayed=true`); another request with the
key is `IDEMPOTENCY_CONFLICT`. A gateway key lives 5 minutes less than `HUB_IDEMPOTENCY_RETENTION`; the purge (at start and
every 5 minutes) then clears its answer, so no output outlives the retention while the Hub runs. `CompleteStream` serves only
`ANSWER` with the same ledger and key (deltas, then the result; ADR-0026). `INSPECT_MATERIAL`, `ANSWER` and `TRIAGE_ASSIGNMENT` take no client instructions: the Hub uses its own prompt
(`hub/pkg/prompts/ops`) and alias; until a prompt ships, or while its alias is not in `HUB_LLM_MODELS`, the operation is
`invalid_argument`. `INSPECT_MATERIAL` takes `{"today","subjects":[{"id","name"}],"material_text"}` as data and returns only
`{"doc_kind","subject","lesson_date","has_homework","has_ticket_list","mentions_email"}`; an answer outside that schema is a
charged, non-retryable `PROVIDER_ERROR` (`hub/internal/domain/llm/services/inspect`). `TRIAGE_ASSIGNMENT` takes
`{"round":1|2|3,"assignment_text","material_text","previous":[{"question","answer"}]}` (no previous in round 1, 1-5 round-1
answers in round 2, 1-10 answers of rounds 1-2 in round 3) and returns `{"work_type","requirements":{"volume","file_format","formatting","variant","deadline","sources"},
"feasibility","feasibility_reason","questions":[{"kind","question","options","default","why"}],"assumptions","helper"}`; rounds 1
and 2 may ask questions, round 3 is the final call, so its questions come back empty (`hub/internal/domain/llm/services/triage`,
same refusal rule). `ANSWER` takes `{"question","fragments":[{"chunk_id","text"}]}` (question up to 500 characters, 1-40
fragments with distinct chunk IDs, up to 8 KiB each and 128 KiB together) and returns
`{"claims":[{"text","cites":[{"chunk_id","quote"}]}],"missing":[]}`: up to 8 claims, each with 1-3 citations of chunk IDs that
were sent, up to 5 unanswered parts, at least one of the two; whether a quote is really in its fragment is the desktop's check
(`hub/internal/domain/llm/services/answer`, same refusal rule). Settings (env or flag):

| Env | Flag | Default | Rule |
|---|---|---|---|
| `HUB_RATE_LIMIT_PER_MINUTE` | `-rate-limit-per-minute` | `30` | 1..6000 calls a minute per API key, on average |
| `HUB_RATE_LIMIT_BURST` | `-rate-limit-burst` | `10` | 1..1000 calls per API key at once |

## Hub agent step
`GatewayService.AgentStep` (ADR-0023): one planner step with tool use. The Hub picks the prompt and tool catalog
(`hub/pkg/prompts/agent/panel/v<N>`, version = `catalog_version`; v1 and v2 are served, v2 is current) and the planner
alias; the client sends neither. Rate limit,
ledger, replay and conflicts work as for `Complete`; echoed tool calls must carry the Hub's seal. The model's raw turn
(thinking with its signature) is kept in `agent_turns` per API key and sent back with the next step; the client never sees
it, and it is purged with the stored answers. A continuation whose kept turn is gone is `failed_precondition`, not charged.
Settings:

| Env | Flag | Default | Rule |
|---|---|---|---|
| `HUB_AGENT_SEAL_KEYS` | none (env only) | none | `version:hex[,version:hex]`, versions 1..255, secrets >= 32 bytes (e.g. `openssl rand -hex 32`); the first signs, the others are still accepted (rotation). Unset: `AgentStep` is `unimplemented`. Secret, never printed |
| `HUB_AGENT_PLANNER_ALIAS` | `-agent-planner-alias` | `planner` | allowlist alias of the planner and of `INSPECT_MATERIAL`; must be in `HUB_LLM_MODELS` when seal keys are set (without it `INSPECT_MATERIAL` is off) |
| `HUB_AGENT_STRONG_ALIAS` | `-agent-strong-alias` | `default` | allowlist alias of `ANSWER` and `TRIAGE_ASSIGNMENT` |

Code architecture rules are enforced by the root `.golangci.yml` (depguard) and `tests/arch_test.go` in each module.
