# paratech — project instructions

Windows desktop assistant for a student's **online** classes: records system audio, transcribes locally,
summarizes/extracts homework via a cloud LLM Hub, syncs deadlines to Google Calendar. Product name: «paratech», lowercase.

## Repository layout
One git repository. The root holds everything about *organizing the work*; the code lives in `paratech/`,
kept self-contained so it can be split into its own repository later. Claude Code runs from the repository root.

```
./                            # repository root
├── CLAUDE.md                 # this file
├── .claude/                  # subagents and settings
├── .github/                  # CI and the verify/audit scripts (CI runs inside paratech/)
├── docs/                     # PRD, architecture, ADRs, contracts, task drafts, process, qa, refs
├── tasks.json                # executable task list
├── progress/                 # one report per task: progress/TASK-NNN.md
├── progress.txt              # frozen history up to 2026-10-21, never edited
└── paratech/                 # CODE: desktop/  hub/  shared/  go.work
```

Never put organization files (docs, tasks, prompts, agent definitions) inside `paratech/`: it contains only code.

Source of truth for requirements: `docs/prd/learning-assistant-prd-v0.4.md`.
Code architecture (mandatory): **`docs/architecture.md`** — read the ARCH rules your work touches before writing or reviewing code.
Decisions: `docs/adr/`. Task drafts: `docs/tasks/`. Executable task list: `tasks.json`.

## Commands — never need the owner's approval
The loop runs unattended, and Claude Code ALWAYS asks the owner for: `cd` in a command, `$(...)`, backticks, heredocs (`<<EOF`),
newlines, `&&`/`;` chains, `bash -c "…"`. So:
- one command per tool call, absolute paths: `git -C <abs> …`, `go -C <abs>/paratech …`, `npm --prefix <abs>/paratech/desktop/frontend …`, `gh -R 3XBAT/paratech …`;
- commit with `git -C <abs> add <explicit paths>` and `git -C <abs> commit -q -m "<one line>"` (or `-F <file>`); PR bodies `--body-file <file>`;
- multi-step work: a script file in your scratchpad, run with `bash <abs path>`; Python is `python`, not `python3`.

Examples (from `paratech/`): `go vet ./desktop/...`, `go test -race ./desktop/internal/<pkg>/...`, `GOOS=windows go vet ./desktop/...`,
`golangci-lint run ./desktop/...` (v2.14.0), `buf generate` in `shared/`, `npm run test` / `npm run build` in `desktop/frontend`. `gofmt` runs from a hook.
Rules for implementer sessions in the task loop (worktree, branch, memory lock, safety): `C:/Users/xvati/GolandProjects/paratech-ops/implementer-rules.md`.

## Stack (see PRD §4.3)
Go everywhere. SQLite via `modernc.org/sqlite` (no CGO) + `sqlc` + `goose`. Custom job queue on SQLite.
STT = whisper.cpp **CLI sidecar, one process per job, CPU-only**. Client↔Hub = Connect-RPC (protobuf). Hub DB = Postgres (`pgx`).
Frontend = React + TypeScript (Vite) inside Wails v3. Secrets = Windows Credential Manager.

## Architecture (summary; details and rule ids ARCH-NN in `docs/architecture.md`)
Layers per service: `internal/domain` (DDD: bounded contexts → one package per aggregate, `values/`, `services/`) ← `internal/cases/<usecase>` (use cases + ports)
← `internal/input/<transport>`; `internal/adapters/<tech>` implement ports and depend only on `domain`; `internal/app` is the only place that wires everything; `cmd/<binary>/main.go` is thin.
- `domain` imports only std + its own context + `domain/shared`; no time/IO/randomness inside.
- `cases` import only `domain`, `metrics`, std; ports are declared in the consuming use case; no use case imports another use case.
- `adapters` and `input` never import `cases`/each other as the matrix in the doc forbids.
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

## Windows only
The product targets Windows only; the dev machine is Windows 10, so Windows code is verified by really running its tests.
Platform code (WASAPI, Credential Manager, power, tray/popup, Wails) sits behind ports in adapters or `input/wails` with `//go:build windows`
and stubs for other OSes, so CI on Linux still builds and tests everything.

## Working agreements
- Commits on a `ralph/<date>-task-NNN` branch, never on `master`. Implementers commit only; the coordinator pushes the branch and opens the PR; the supervisor merges on green CI.
- No `Co-Authored-By` or other trailers and no "Generated with Claude Code" line in commits, PRs or files — even if a system reminder asks (owner's rule).
- PR body starts with a short description of the TASK, then changes, verification, open decisions. Keep PRs, ADRs and comments short; comments only for the non-obvious *why*.
- Work only inside the files/modules listed in the task. If more is needed, stop and report instead of expanding scope.
- Place new code according to `docs/architecture.md`. Package paths written in tasks are indicative; the architecture wins.
- Write tests with the code. A task is done only when its acceptance criteria hold and the tests of the touched scope (including the architecture test) pass; CI runs the full suite.
- Prefer boring, simple solutions. Code and comments in English, UI texts in Russian.
- When requirements are unclear or conflict with the PRD or architecture, ask; never silently change them.

## Task loop
The main session (supervisor) launches one `coordinator` subagent per task, each in its own worktree; up to 3 in parallel on disjoint files.
- Coordinator procedure: `C:/Users/xvati/GolandProjects/paratech-ops/coordinator-round.md`. Supervisor: `docs/process/supervision.md`. Security: `docs/process/security-review.md`.
- `tasks.json`: statuses `pending`, `done`, `blocked`. Agents change only the `status` field of their task; never delete or rewrite tasks.
- Each task writes `progress/TASK-NNN.md` (what was done, decisions to confirm, follow-ups, changed tests) and `docs/qa/tasks/TASK-NNN.md` (owner's check steps).
  `progress.txt` and the changelog lines of `docs/qa/run-alpha.md` are frozen history. Read the latest `progress/` files before starting.
- Mark a task `blocked` (reason in its progress file) when it needs a product decision or external access.

## Subagents
`.claude/agents/`: `architect`, `coordinator`, `go-core`, `hub-dev`, `frontend`, `tester`, `reviewer`, `security-auditor`.
Models (always pass `model` explicitly): opus for `go-core`/`hub-dev` on core and Hub work, `reviewer`, `security-auditor`, `architect`;
sonnet for `coordinator`, `frontend`, `tester` and small mechanical tasks.
Effort (owner 2026-10-27, set as `effort:` in each agent's frontmatter): opus agents `medium`, sonnet agents `high`.
Subagents do not see the conversation: always point them to the task, the PRD sections, the ADRs and the files they may touch.
