# TASK-415: agenteval — section «odd» and its rubric (code only)

- **Agent:** go-core (sonnet)
- **Label:** test
- **Depends on:** —
- **PRD sections / AC:** §6 (agent), AC-11; design §3.3; ADR-0021, ADR-0033
- **Агент:** — (test tooling)
- **Branch:** ralph/<date>-task-415

## Goal
The live case file gets a section `odd` (unusual requests) and four rubric fields the run scores, so TASK-416 can
add the cases. CI keeps checking the file and the runner on the fake Hub.

## Allowed to change
- `paratech/desktop/tests/agenteval/{cases.go,score.go,report.go,runner_test.go,score_test.go,live_test.go,README.md}`
- `paratech/desktop/tests/agenteval/testdata/live_cases.json` (only `"odd": []`)

## Contract
- `CaseSet.Odd []Request` (`json:"odd"`), bounds `MinOdd = 0` (TASK-416 raises it to 25), `MaxOdd = 40`; IDs unique
  across requests, cannot_do, odd and questions. An odd case may list `cannot_do` in `also` (requests may not).
- New `Request` fields (any section; checked by `ParseCaseSet`):
  - `forbid []string` — catalog tools no call of the run may be (each must exist in the catalog);
  - `expect_any []string` — non-blank phrases; at least one card text must contain one (same normalisation as `avoid`);
  - `due_any []string` — accepted `due` of a `create_assignment` / `edit_due` call, `""` = the call gives no due;
    needs one of those tools in `tools`, or a class `questions`/`reply` in `class`/`also`;
  - `today` — `YYYY-MM-DD`, the base date of this case (default `liveToday`); `liveBase` takes it.
- `Outcome`: `Ended` (the run stopped at a card or action within `maxLiveSteps`, no run error), `Dues` (the `due` of
  each `create_assignment` / `edit_due` call, `""` none).
- `Metrics`: `Odd, OddClassOK, OddToolsOK`, `Ended` (over every request, odd and cannot_do), `Forbids, ForbidsOK`,
  `Expects, ExpectsOK`, `DueCalls, DueOK` (cases with such a call, and those whose every due is in `due_any`).
- Report rows (Russian, like the others): «Необычные запросы: класс плана», «Необычные запросы: инструменты»,
  «Закончились карточкой или действием», «Без запрещённых инструментов», «Ожидаемые слова в ответе», «Срок: верная
  дата»; misses list each failure with the case id and text.

## Acceptance criteria
- [ ] Field checks: unknown forbidden tool, blank phrase, `due_any` without a due tool or questions/reply, bad `today`,
      `cannot_do` in `also` of a request — each rejected by `ParseCaseSet` (table test).
- [ ] Scoring tests: forbidden tool called → not ok; phrase found / not found; due ok / wrong / none; a run that never
      reaches a card in `maxLiveSteps` is not `Ended`.
- [ ] `TestRunnerOnTheFakeHub`: odd cases are sent like the others (the «every case can be sent» loop covers them); one
      scripted odd case with `today` shows the date in the request's base.
- [ ] README: the section, the fields, how to run only the odd cases (`-Only` of `paratech-ops/run-agent-live.ps1`).
- [ ] `go test -race ./desktop/tests/agenteval/...`, `go vet ./desktop/...`, `golangci-lint run ./desktop/...`.

## Notes
The run still executes nothing; a C tool ends the run with `{"ok":true}`, so `forbid` of a C tool is how «no silent
send/delete» is scored. No new dependency.
