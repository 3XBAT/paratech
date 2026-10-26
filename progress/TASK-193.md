# TASK-193 (PAUSED, not pushed)

Done (commit 3f8be75e, frontend only): all 7 items implemented; npm test 729/729 and build OK; tests in test/followups193.test.mjs.
Changed existing tests: turnkey.test.mjs (step help "unavailable" -> error), homeworkturnkey.test.mjs (state now carries work).
Limits: panel start-failure reason shows only after «Повторить» (core stores no problem); "already sent" warning is session-local (no DTO flag); ErrTurnkeyNoStepHelp text kept for a Go-source guard test.

Left: reviewer + security-auditor (parallel), fix findings, docs/qa/tasks/TASK-193.md, tasks.json status, final progress, fetch/merge origin/master (bindings/turnkey.ts may conflict with TASK-194: keep both), push, PR, audit-pr.sh.
