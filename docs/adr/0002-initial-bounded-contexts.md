# ADR-0002: Initial bounded contexts, single lint config, proto path

- **Status:** Accepted
- **Source:** TASK-002 (architecture scaffold and automatic checks); `docs/architecture.md` sections 3, 9, 10, 12

## Context
TASK-002 creates the layer skeleton of `desktop` and `hub` and the automatic enforcement of `docs/architecture.md`.
Three points need a recorded decision: which bounded contexts exist at the start, where the lint rules live,
and a path in the task list that differs from the code.

## Decision

### 1. Initial context map
Taken from section 12 of `docs/architecture.md`; aggregates and domain services are added by later tasks, not by the scaffold.
The scaffold contains only the template contexts `desktop/internal/domain/jobs` and `hub/internal/domain/quota`
(each with `doc.go` and a glossary) and the shared kernel `internal/domain/shared` in both modules.

**desktop** (`paratech/desktop/internal/domain/...`)

| Context | Aggregates | Domain services |
|---|---|---|
| `recording` | `recording` (states, chunks, limit) | `limits` |
| `jobs` | `job` (PRD 7.2 statuses, attempts, idempotency key) | `backoff`, `powerpolicy` |
| `transcription` | `sttmodel` (versions, pin), `transcript` | `modelselection` |
| `schedule` | `lesson` | `importplan` (preview and conflicts) |
| `coursework` | `assignment`, `homeworkdraft` (versions) | `contextselection` (minimal LLM context) |
| `delivery` | `delivery` (including UNKNOWN and reconciliation) | |
| `settings` | `settings` | |

Example use cases: `cases/recordingsession`, `cases/jobrunner`, `cases/transcribe`, `cases/modelcatalog`, `cases/schedulesync`, `cases/homeworkflow`, `cases/senddelivery`.

**hub** (`paratech/hub/internal/domain/...`)

| Context | Aggregates | Domain services |
|---|---|---|
| `access` | `apikey` | |
| `quota` | `account` (balance, reservations, journal), `reservation` | |
| `llm` | | `modelallowlist`, `requestlimits` |

Example use cases: `cases/gateway`, `cases/quota`, `cases/keys`. Adapters: `postgres`, `anthropic`. Input: `input/connect`, `input/cli`.

Adding a context, an aggregate or a use case needs no ADR as long as it fits section 12; a new top-level directory in `internal/` does (ARCH-01).
A new context must also get a per-context `depguard` rule in `paratech/.golangci.yml` (see below); until then `tests/arch_test.go` still enforces ARCH-11 for it.

### 2. One workspace-level `.golangci.yml`
`docs/architecture.md` section 10 speaks of a `.golangci.yml` per module. We use **one** config, `paratech/.golangci.yml`,
because `golangci-lint` is started from the workspace root `paratech/` (CI, Makefile-less local use), where only that file is found,
and one file avoids drift between two copies. Rules are scoped with `files` globs (`**/desktop/internal/domain/**`, ...), one rule set per module and layer.

- `depguard` implements the matrix of section 9. It cannot express "only the own context/package" for names unknown in advance.
  Known domain contexts get a per-context rule; cases use an allow list. Cross-adapter and cross-transport imports (ARCH-30, 40)
  and contexts added later (ARCH-11) are checked by `tests/arch_test.go`. `time.Now` and `import "C"` are checked only by `tests/arch_test.go`.
- Each module has its own `tests/arch_test.go` (package `tests`, std library and `go/parser` only). The logic is duplicated on purpose
  (no shared test module); the desktop copy additionally checks `import "C"`. Both also verify that no organization files live inside `paratech/`.
- Beyond the letter of the matrix the checks also: forbid `crypto/rand` next to `math/rand` in `domain` and `cases` (ARCH-15, 24);
  allow `testenv` only in `_test.go` files of `cases`; keep `cmd/*` limited to `config` and `internal/app` (ARCH-51); forbid `desktop` and `hub` importing each other.
- Bare `golangci-lint run ./...` does **not** work in the workspace root (`paratech/` is not a module). Use
  `golangci-lint run ./desktop/... ./hub/... ./shared/...` (and `GOOS=windows golangci-lint run ./desktop/...` for Windows-only files).
  Test step 3 of TASK-002 and the README use the older `./...` wording; the explicit list is the working form.

### 3. Protobuf path
TASK-004 refers to `shared/proto/hub/v1`. The actual layout, created in TASK-001 and required by `buf` lint
(package `paratech.hub.v1` must match the directory), is `shared/proto/paratech/hub/v1`, with generated code in `shared/gen/paratech/hub/v1`.
TASK-004 and later tasks must use the actual path.

## Consequences
- Violating the matrix fails both `go test ./desktop/tests/...` / `./hub/tests/...` and `golangci-lint`; the architecture test is the authority where they differ.
- Section 10 of `docs/architecture.md` should be updated by the architect to mention the single root config (this ADR does not edit it).
- Adding a third-party dependency to `domain` or `cases` is rejected by both checks until an ADR extends the allow lists.
