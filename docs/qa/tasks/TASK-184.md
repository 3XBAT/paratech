# QA TASK-184: desktop on catalog 4

Needs the Hub with TASK-182 (panel v4) deployed first.

1. Tests: `go -C <repo>/paratech test -race ./desktop/internal/domain/agent/... ./desktop/internal/input/agent/... ./desktop/tests/...` pass; `GOOS=windows go vet ./desktop/...` is clean.
2. Panel: «Сделай черновик по заданию 2-4»: the agent offers a plan with triage of the assignment, not a draft job. Drafting stays on the assignment page («Быстрый черновик»).
3. Panel: «Сделай презентацию на тему X» with no assignment: a plan with a topic step may be offered; after «Да» the step answers that it is unavailable and nothing starts (until TASK-188).
4. Journal: older entries of a generated draft still show, and Undo of a running draft job still cancels it.
5. A request stopped before the update: «Продолжить» / «Повторить» end with a «не удалось» stop (no hang); a new request works.
