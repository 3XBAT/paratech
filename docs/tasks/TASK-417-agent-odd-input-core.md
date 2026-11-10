# TASK-417: agent — odd input on the fake Hub (level 1) and plain problems for an empty or too long request

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** —
- **PRD sections / AC:** §6, AC-11; ADR-0021, ADR-0026 (addendum 2026-11-08)
- **Агент:** — (the request path itself)
- **Branch:** ralph/<date>-task-417

## Goal
Pin what the core does with unusual student input, end to end on the real use cases, SQLite and Hub client with the
fake Hub (level 1, CI every run), and replace the generic `failed` of `AgentService.Ask` for an empty or too long text
by plain problems the panel can explain.

## Found while planning (verify first)
- `values.MaxStudentTextBytes = 4 KiB` (bytes) but the panel field allows `maxDraftLength = 4000` **characters**
  (`frontend/src/agentMentions.ts`): Cyrillic is 2 bytes, so a ~2100-character Russian request is refused by
  `values.NewStudentText`, and `AgentService.Ask` maps every such error to `failed` (`agent.go` `started`).
- An empty text without uploads is refused the same way.

## Allowed to change
- `paratech/desktop/tests/agent_odd_input_test.go` (new)
- `paratech/desktop/internal/domain/agent/values/result.go` (sentinels only: `ErrEmptyText`, `ErrTextTooLong`, both
  wrapping `ErrInvalidText`) + its test
- `paratech/desktop/internal/input/wails/agent.go` (Ask mapping only) + `agent_test.go`
- `paratech/desktop/frontend/src/bindings/agent.ts` (accept the two new problems in `parseAskResult` only) + its test

## Cases (one table test; each asserts what the Hub received, the Ask result and the panel update)
| input | expected |
|---|---|
| `""`, `" \n\t "`, no uploads | Ask problem `empty`; no Hub request, no chat or request created |
| only spaces + one upload | accepted (upload-only request, existing rule) |
| `🙂`, `👍🏽`, `👨‍👩‍👧`, `🇷🇺` | accepted; the Hub's student text equals the input byte for byte; a scripted reply card arrives |
| `@Физика` + mention of that subject | accepted; scope mention current = true, text as typed |
| `""` + one mention | problem `empty` (the panel always keeps the «@Название» in the text; owner question 1) |
| 2048 × `я` (exactly 4096 bytes) | accepted |
| 2049 × `я` (4098 bytes); 4000 × `я` (the field's limit today) | problem `too_long`; nothing sent |
| 4000 Latin letters | accepted |
| text with NUL | problem `invalid` (never `failed`) |
| a mention of a deleted object | dropped, the request goes on (existing rule) |

## Acceptance criteria
- [ ] Every row above holds on the fake Hub (`testenv.NewFakeAgentHub`), including «nothing sent» via `hub.Requests()`.
- [ ] `AgentService.Ask`: `values.ErrEmptyText` → `empty`, `values.ErrTextTooLong` → `too_long`, other
      `values.ErrInvalidText` → `invalid`; `failed` only for real failures; the warning log line has no student text.
- [ ] `bindings/agent.ts` accepts `empty` and `too_long` (texts and the field limit are TASK-418).
- [ ] `go test -race ./desktop/internal/domain/agent/... ./desktop/internal/input/wails/... ./desktop/tests/...` and the
      architecture test; `GOOS=windows go vet ./desktop/...`; `golangci-lint run ./desktop/...`;
      `npm --prefix paratech/desktop/frontend run test`.

## Notes
A row that shows a defect outside these files: report in progress and stop (CLAUDE.md), do not widen the scope.
