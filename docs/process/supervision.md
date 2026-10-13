# Supervision of the coordinator

Two levels of control. The coordinator (agent) runs the task loop and supervises implementers, reviewers and security agents.
The supervisor (the main Claude session talking to the owner) audits the coordinator independently, after each task and at the end of a batch.

## What the supervisor does
1. Launches the coordinator with an explicit batch ("tasks A, B, C, one PR each") and the limits from `.claude/agents/coordinator.md`.
2. On every coordinator report, does not trust it: runs `.github/scripts/audit-pr.sh <pr>` and compares with the report.
3. Spot checks: `gh pr view`, `gh pr checks`, the diff of one or two files per PR, the tail of `progress.txt`, `git log` on `master` (only merge commits of loop PRs).
4. On any flag or mismatch: stops the coordinator, explains the problem in the next prompt, and fixes it (revert via a new PR, never force-push).
5. Stays reachable to the owner: reports batch results and open decisions; does not ask routine questions.

## Audit checklist (automated by `audit-pr.sh`)
| Check | Why |
|---|---|
| `tasks.json` diff changes only `status` lines | agents may change statuses only |
| `progress.txt` is append-only (no removed lines) | shared memory of the loop |
| Sensitive paths touched: `.claude/`, `.github/`, `CLAUDE.md`, `ralph.sh`, `docs/architecture.md`, `docs/prd/`, `paratech/.golangci.yml`, `go.work`, `docs/process/` | scope creep and tampering with rules and CI |
| New `t.Skip`, `nolint`, `//go:build ignore`, deleted test files | weakened verification |
| `go.mod` changes (new `require`) | dependency policy (hard rule 9) |
| Secret-like strings added (keys, tokens, private key headers) | hard rule 4 |
| CI state of the head commit | no merge on red or pending |
| Files changed outside `paratech/`, `docs/`, `tasks.json`, `progress.txt` | unexpected locations |

A flag is not always a violation: tasks may legitimately touch some of these (an ADR in `docs/`, a dependency named in the task). The coordinator must have explained each flag in its report; the supervisor decides.

## Red lines (stop and revert)
Force-push or history rewrite, direct push to `master`, merge with failing CI, secrets in the repo, changed CI/lint/agent definitions without an explicit task step, deleted or skipped tests, `tasks.json` edited beyond status, `progress.txt` entries removed.

## Reporting
The supervisor tells the owner, briefly: which tasks were merged, which are blocked and why, flags found and how they were resolved, decisions left open.
