# Supervision of the task loop

The supervisor is the main Claude session talking to the owner. It launches one `coordinator` per task, audits every result and merges.
Current state and queue live in the supervisor's handoff file (`paratech-ops/handoff-*.md`); decisions for later tasks in `paratech-ops/supervisor-decisions.md`.

## Running the loop
1. Pick ready tasks: `pending`, all dependencies `done`, no open PR they depend on. Up to 3 coordinators in parallel, each in its own worktree
   (`paratech-team2..6`), on disjoint files; desktop migrations are numbered at push time (the second to merge renumbers).
2. Launch the `coordinator` (model sonnet, in the background) with: the task id, the worktree, the branch name, the supervisor decisions that apply,
   what parallel tasks own. Its procedure is `paratech-ops/coordinator-round.md`.
3. On the coordinator's report do not trust it: check the PR diff scope, flags of `.github/scripts/audit-pr.sh <pr>`, the decisions it took.
4. Merge with `bash paratech-ops/wait-merge.sh <pr> "<regex of explained flags>"`: it waits for CI and merges only when every check is SUCCESS.
   Never pipe verify or check output through `tail`/`grep` before testing it for green.
5. Heavy local steps (many-package tests, lint, npm build, the owner's build) go through `paratech-ops/heavy.sh` (memory lock, 16 GB machine).
   Never edit a running script in place: write `*.new`, `bash -n`, then `mv -f`.

## Audit checklist (automated by `audit-pr.sh`)
| Check | Why |
|---|---|
| `tasks.json` diff changes only `status` lines | agents may change statuses only |
| `progress.txt` has no removed lines (and should not change at all) | frozen history; reports go to `progress/TASK-NNN.md` |
| Sensitive paths: `.claude/`, `.github/`, `CLAUDE.md`, `docs/architecture.md`, `docs/prd/`, `docs/process/`, `paratech/.golangci.yml`, `go.work` | tampering with rules and CI |
| New `t.Skip`, `nolint`, `//go:build ignore`, deleted test files | weakened verification |
| `go.mod` changes (new `require`) | dependency policy (hard rule 9) |
| Secret-like strings | hard rule 4 |
| CI state of the head commit | no merge on red or pending |
| Files outside `paratech/`, `docs/`, `tasks.json` | unexpected locations (`progress/TASK-NNN.md` is expected) |

A flag is not always a violation (an ADR in `docs/`, a dependency named in the task). The coordinator explains each flag; the supervisor decides.

## Red lines (stop and revert via a new PR, never force-push)
Force-push or history rewrite, push to `master`, merge with failing CI, secrets in the repo, changed CI/lint/agent definitions without an explicit task step,
deleted or skipped tests, `tasks.json` edited beyond status.

## Resuming after a limit or a pause
State is in git, `tasks.json`, `progress/` and the handoff file. Subagents that were running are lost; a paused coordinator leaves a WIP commit and its
next step in `progress/TASK-NNN.md` on its branch. Check `gh pr list`, the worktrees (`git -C <wt> status`), then continue or relaunch.

## Reporting to the owner
Briefly: what merged, what runs, what is blocked and why, decisions taken on the owner's behalf, questions that really need the owner.
The owner reviews PRs on GitHub afterwards; comments are answered in the PR and fixed on the same branch.
