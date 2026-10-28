# TASK-317 — check steps

Domain only: nothing new is visible in the app (the Hub still shows plans of up to 8 steps until catalog 10, TASK-325).

1. From a cold checkout run `go -C <repo>/paratech test -race -count=1 ./desktop/internal/domain/agent/... ./desktop/internal/cases/agentturn/... ./desktop/internal/input/agent/... ./desktop/tests/...`. Expected: all ok, including the architecture test.
2. Run `TestLongPlanRunsWithoutWriteLimit` in `desktop/internal/domain/agent/thread/planbudget_test.go`: a plan of 30 writes runs with no stop, `Writes` stays 0, the closing reply step runs.
3. In the same file (`TestPlanSize`, `TestStepLimitsByMode`) see the limits: 201 steps are refused, 60 planner steps under a plan and 8 without, 1 000 000 and 160 000 input tokens.
4. In the app on a normal request with 8 steps and no plan nothing changes: «Заведи предмет X» still works, a second write in one request still shows the plan card.
