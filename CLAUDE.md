# Learning Assistant — project instructions

Windows desktop assistant for a student's **online** classes: records system audio, transcribes locally,
summarizes/extracts homework via a cloud LLM Hub, syncs deadlines to Google Calendar.

## Repository layout
One git repository (for now). The root holds everything about *organizing the work*; the code lives in the `paratech/` subdirectory,
which is kept self-contained so it can be split into its own repository later. Claude Code and `ralph.sh` run from the repository root.

```
./                            # repository root
├── CLAUDE.md                 # this file
├── .claude/                  # subagents and settings
├── .github/workflows/        # CI (runs inside paratech/)
├── docs/                     # PRD, architecture, ADRs, task drafts, process, refs
├── tasks.json, progress.txt  # ralph loop memory
├── ralph.sh                  # autonomous loop
└── paratech/                 # CODE: desktop/  hub/  shared/  go.work
```

Path convention: `docs/`, `tasks.json`, `progress.txt` are relative to the repository root; `desktop/`, `hub/`, `shared/` and every Go/npm/buf command
from tasks and test steps are relative to `paratech/` — **run them with `cd paratech` first**. Stage and commit code with `git -C paratech add -A .` and `git -C paratech commit`.
Never put organization files (docs, tasks, prompts, agent definitions) inside `paratech/`: that directory must contain only code, so it can be extracted cleanly.

Source of truth for requirements: `docs/prd/learning-assistant-prd-v0.4.md`.
Code architecture (mandatory): **`docs/architecture.md`** — read it before writing or reviewing any code.
Decisions: `docs/adr/`. Task drafts: `docs/tasks/`. Executable task list: `tasks.json`.

## Stack (see PRD §4.3)
Go everywhere. SQLite via `modernc.org/sqlite` (no CGO) + `sqlc` + `goose`. Custom job queue on SQLite.
STT = whisper.cpp **CLI sidecar, one process per job, CPU-only**. Client↔Hub = Connect-RPC (protobuf). Hub DB = Postgres (`pgx`).
Frontend = React + TypeScript (Vite). Secrets = Windows Credential Manager.

## Architecture (summary; details and rule ids ARCH-NN in `docs/architecture.md`)
Layers per service: `internal/domain` (DDD: bounded contexts → one package per aggregate, `values/`, `services/`) ← `internal/cases/<usecase>` (use cases + ports)
← `internal/input/<transport>`; `internal/adapters/<tech>` implement ports and depend only on `domain`; `internal/app` is the only place that wires everything; `cmd/<binary>/main.go` is thin.
- `domain` imports only std + its own context + `domain/shared`; no time/IO/randomness inside.
- `cases` import only `domain`, `metrics`, std; ports are declared in the consuming use case; no use case imports another use case.
- `adapters` and `input` never import `cases`/each other as the matrix in the doc forbids; platform code (`//go:build windows`) only in adapters/`input/wails`, with stubs for other OSes.
- Invariants and state machines live in aggregates, never in use cases, adapters or UI.
Violating `ARCH-NN` rules is a review blocker. Changing the rules requires an ADR.

## Hard rules (violating any of these is a bug)
1. The desktop binary has **no CGO** unless an ADR says otherwise.
2. **No cloud STT.** Raw audio never leaves the machine.
3. **LLM output is untrusted.** It never triggers side effects (email, files, calendar) directly:
   LLM proposes structured result → Core validates → policy/user confirmation → adapter acts.
4. No long-lived credentials or tokens in SQLite or plaintext files.
5. All file and DB access goes through ports implemented by adapters (no `os`/`database/sql` use in `domain` or `cases`).
6. Local data is the system of record; Google Calendar is only an external view.
7. Jobs with external side effects must be idempotent. Email delivery with unknown outcome is reconciled, never blindly retried.
8. No user-login system in MVP; the Hub is guarded by per-tester API keys (PRD §12).
9. Do not add a dependency without recording it in an ADR or the task file. Do not invent library APIs: check `go.mod`, `docs/refs/`, or the library source.

## Windows vs macOS
Product target is Windows; much development happens on macOS. Put platform-specific code (WASAPI, Credential Manager, power status, tray/popup behavior)
behind ports, with `//go:build windows` adapters and stub/fake implementations for other OSes so tests run everywhere.
Always check that Windows code compiles: `cd paratech && GOOS=windows go vet ./desktop/...`.

## Commands (run from `paratech/`)
- Build/vet/test a module: `go vet ./desktop/... && go test -race ./desktop/...` (same for `hub`, `shared`)
- Formatting: `gofmt -w .` (also run automatically by a hook)
- Lint (when installed): `golangci-lint run ./...`
- Contracts: `buf generate` / `buf breaking` in `shared/` (once set up)
- Frontend: `npm run build` / `npm run test` in `desktop/frontend`

## Working agreements
- Small, reviewable commits on a non-default branch (not `master`) of the code repo (`task/...` when run manually, `ralph/...` when run by `ralph.sh`). Do not push; the user pushes.
- Work only inside the files/modules listed in the task. If more is needed, stop and report instead of expanding scope.
- Place new code according to `docs/architecture.md`. Package paths written in tasks are indicative; the architecture wins.
- Write tests with the code. A task is done only when its acceptance criteria hold and tests (including the architecture test) pass.
- Prefer boring, simple solutions. The MVP goal is "works and works reliably", not optimization for specific hardware.
- Code and comments in English. Keep comments for non-obvious *why*.
- When requirements are unclear or conflict with the PRD or architecture, ask the user; never silently change them.
- Final report of every task: what changed, what is not done, which PRD AC ids are covered, how it was verified.

## Ralph loop (autonomous mode)
`./ralph.sh` (run from the repository root) runs fresh agent sessions in a loop; each session does exactly ONE task from `tasks.json` and commits the code.
Memory between sessions is only files and git:
- `tasks.json` — task list (generated from the PRD with `docs/process/prd-to-tasks.prompt.md`). Statuses: `pending`, `done`, `blocked`.
  Agents may change only the `status` field; never delete or rewrite tasks.
- `progress.txt` — append-only log; read the latest entries before starting, append an entry when finishing (format in the file header).
- `docs/tasks/` — human-written detailed specs/drafts; `tasks.json` is what the loop executes.
The script itself commits `tasks.json`/`progress.txt` after each iteration; agents commit only code (`git -C paratech add -A .`).
Mark a task `blocked` (with the reason in `progress.txt`) when it cannot be finished autonomously: it needs verification on real Windows,
a user decision, or external access. Never mark `done` without running every `test_steps` item.
Options: `RALPH_AGENT`, `RALPH_MAX_ITERATIONS`, `RALPH_MAX_STALLS`, `RALPH_REVIEW` (see the top of `ralph.sh`).

## Subagents
Defined in `.claude/agents/`: `architect`, `go-core`, `hub-dev`, `frontend`, `tester`, `reviewer`.
Subagents do not see the conversation: always point them to the task file, the PRD sections, `docs/architecture.md` and the files they may touch.
