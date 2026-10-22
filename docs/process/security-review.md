# Security review process

Every task goes through a security review in addition to the architecture and quality review by `reviewer`.
One agent looks for holes; the coordinator judges each finding, and confirmed High/Medium findings are fixed.

## Roles
| Step | Agent | Rule |
|---|---|---|
| 1. Find | `security-auditor` (read-only) | Reads the task diff, reports findings with an attack path and a confidence score. Never fixes. |
| 2. Judge | the coordinator | Reads each finding against the code; a finding it doubts is recorded with the reason, not silently dropped. |
| 3. Fix | the implementing agent (`go-core`, `hub-dev`, `frontend`) | Fixes High and Medium findings, adds a regression test that fails before the fix. |
| 4. Re-check | `reviewer` on the fix commits | Confirms the fix closes the attack path and introduces no new one. |

The coordinator runs the steps, keeps the findings list and records the outcome in `progress/TASK-NNN.md`
(`Security: N found, K fixed, J accepted, D doubted` plus one line per accepted or doubted finding).

## When
- After every task, after the normal review (`reviewer`) and before the task is marked `done`.
- On demand for a whole branch or an area (for example "the Hub contract" or "everything that handles file paths").
- Tasks that touch trust boundaries (Hub, API keys, quotas, file or path handling, subprocesses, LLM output handling, email or calendar side effects, CI)
  get the full pipeline. For tasks that only touch tests, docs or pure refactors, the auditor pass alone is enough.

## Threat model in one page
- **Assets:** lecture recordings and transcripts (never leave the machine as audio, hard rule 2), homework drafts, mailbox and Google Calendar access, per-tester Hub API keys and quota.
- **Attackers:** (a) the LLM or content derived from lectures (prompt injection through transcripts, hard rule 3); (b) a malicious or buggy imported file (ICS, CSV, XLSX) or downloaded model; (c) another tester on the Hub; (d) another local process or user; (e) a malicious dependency or CI input.
- **Controls:** LLM output is validated by Core and confirmed by policy or the user before any side effect; secrets only in Windows Credential Manager; Hub keys scoped per tester; the client cannot choose URLs or hosts; typed IDs and `ArtifactRef` for paths; idempotent side effects.

## Severity
- **High:** exploitable path to code execution, secret or recording disclosure, auth or tenant-isolation bypass, or an LLM-triggered side effect without confirmation.
- **Medium:** needs specific conditions but has significant impact (for example cross-tester metadata exposure, file write outside the data root under a plausible condition).
- **Low:** defense in depth. Recorded, never blocks a task.
Not security findings: denial of service, resource exhaustion, rate limiting, missing hardening without an attack path, outdated dependency versions (handled by `govulncheck`), theoretical races, docs or tests only.

## Outcomes
- **Blocks `done`:** a CONFIRMED High or Medium finding that is not fixed, or an accepted risk that the user has not approved.
- **Accepted risk:** the coordinator may not accept a High or Medium risk alone; it asks the user and records the decision in an ADR or `progress/TASK-NNN.md`.
- **Low and doubted-with-note:** logged in `progress/TASK-NNN.md` for later hardening.

## Automatic layers (no agent needed)
- `gosec` linter in `.golangci.yml` and `govulncheck` in CI catch known bad patterns and known vulnerable dependencies.
  `govulncheck` runs through `.github/scripts/vulncheck.py` for every module. A finding that does not apply to the product may be listed in `.github/vulncheck-allow.txt` only with a written reason checked by the reviewer, with a reason and a removal condition; stale entries fail CI. Prefer bumping Go or the dependency.
- The Anthropic `Security Guidance` plugin (hooks on edits and on stop, if enabled in the session) adds pattern warnings; treat its warnings as input for the auditor, not as a verdict.
- `claude-security` (Anthropic plugin) can be used for an occasional deep scan of the whole repository; its findings go through the same judge step.

## Report format of the auditor
See `.claude/agents/security-auditor.md`.
