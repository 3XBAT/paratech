# Coordinator runbook

How the task loop is run by Claude Code in a session (not by `ralph.sh`). A new session can continue the loop from this file.

## Start of a session
1. Read `CLAUDE.md`, `docs/architecture.md` and this file. Memory notes of the project (if present) complement them.
2. Find the state: `git status`, `git branch --show-current`, `gh pr list --repo 3XBAT/paratech`, the tail of `progress.txt`, `tasks.json` (`pending`/`done`/`blocked`).
3. Unfinished work of a previous session: uncommitted changes on a `ralph/...` branch. Inspect them, then continue or discard. Subagents that were running are lost: start them again.
4. Never run two coordinator sessions at once (shared working tree and branches).

## Per task (one task = one branch = one PR)
1. Branch `ralph/<date>-task-NNN` from fresh `origin/master`. Pick a `pending` task whose dependencies are `done` (critical first). Tasks needing a real device, a user decision or external access: mark `blocked` with the reason in `progress/TASK-NNN.md`.
2. Implement with the profile agent (`go-core`, `hub-dev`, `frontend`) and pass `model: "opus"` explicitly. The prompt must carry: the task id, the files/ADRs/PRD sections to read, the style rules, the verification commands, the commit format. Subagents do not see the conversation.
3. Verify (since 2026-10-21): while working and after fixes run only the touched scope — `go vet`/`go test -race` of the changed packages and their dependants, `golangci-lint` on them (both GOOS when Windows code is touched), npm test/build when the frontend is touched, sqlc/buf drift when touched. One full `.github/scripts/verify.sh` before the first push is optional; CI runs everything and the merge waits for green CI.
4. Review: `reviewer` (diff vs master). In parallel `security-auditor` (no skeptic since 2026-10-21; `docs/process/security-review.md`). Fix blocker/high/medium findings with the implementing agent, then re-run the touched scope. Low findings and deferred items go to `progress/TASK-NNN.md`.
5. Bookkeeping: set the task `status` to `done` in `tasks.json` (status only), write `progress/TASK-NNN.md` (what was done, decisions to confirm, follow-ups) and the owner-check steps to `docs/qa/tasks/TASK-NNN.md` (no edits to shared logs), commit as `ralph: TASK-NNN done (...)`.
6. Push, open the PR with `gh pr create` (short body: task, changes, verification, open decisions; no generated-with line). Merge when CI is green (`gh pr checks`) with `gh pr merge --merge`. Do not use `--auto`. Do not force-push. Do not merge PRs you did not create for the loop.
7. Next task: branch from fresh master again. While CI runs, the next implementation may start on a branch from `origin/master`; run `verify.sh` before finishing it (it rebases and resolves generated-file conflicts).

## Conventions
- Comments short; blank lines between logical blocks; operation names as constants (see `adapters/filestore/store.go`).
- Commits: `TASK-NNN: ...` plus the `Co-Authored-By` trailer; bookkeeping commit `ralph: TASK-NNN done`.
- Decisions: decide yourself, record open ones in the PR body and `progress.txt` (no questions to the owner for routine choices). Stop and ask only for product/PRD conflicts, real-device checks, anything outside the task's scope, installing software, entering credentials.
- The owner reviews PRs on GitHub afterwards; comments are answered in the PR and fixed on the same branch. Do not resolve threads unless asked.
- Dev machine is Windows 10 with Git Bash: Windows-only code is verified by really running tests; `rm -rf` is denied by `.claude/settings.json`; `python3` is a Store stub, use `python`; the `golangci-lint` in `~/go/bin` may be v1: v2.14.0 must be first on PATH.

## Limits and resuming
On a usage limit the session just stops. State is in git, `tasks.json` and `progress.txt`; to resume: section "Start of a session". Commit and push finished stages often so little is lost.

## Open points carried between tasks
Kept in the last entries of `progress.txt` (for example single-instance guard before wiring `Recover`, the handler contract for side-effect jobs in TASK-044, filestore path resolver, ASCII-only data root decision in TASK-025).

## Two-level control
Batches are normally run by the `coordinator` agent (`.claude/agents/coordinator.md`); the main session acts as supervisor and audits every coordinator report with `.github/scripts/audit-pr.sh <pr>` (`docs/process/supervision.md`). Both follow this runbook.
The coordinator handles one task per run; the supervisor starts it again for the next task.
