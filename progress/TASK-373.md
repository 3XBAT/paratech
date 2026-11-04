# TASK-373 — E2E hooks: file dialogs, link/file opener, clipboard

Branch ralph/20261104-task-373. Go only. Commits: 578cf0be (hooks), 07703415 (call link), 1acf4a3f (review fixes).

## What was done
- New adapter `adapters/e2ehooks` (std + domain only; `doc.go` has the file formats). Open dialogs (schedule, model, material, notes, recording)
  take the next line of `<e2e-dir>/picks.jsonl`; consumed lines are appended to `consumed.jsonl` (survives a restart). Only lines ending in
  `\n` count (a half-written trailing fragment is not consumed). Save dialogs (bundle, transcript, summary, knowledge, docx) answer
  `<e2e-dir>/saved/<safe name>` (extension kept when a long name is cut). The recorder appends `{kind,value,at}` to `opened.jsonl`
  for `link`, `file`, `clipboard`, `call` and starts nothing (a test checks the package imports no os/exec, syscall, net).
- Path confinement: by name (relative only, `fixtures` first, no `..`, `:`, UNC, `\\?\`, device names, `~`, trailing dot/space, control chars),
  then `Lstat` of every component (links and junctions refused, last must be a regular file), then `EvalSymlinks` must stay below the resolved
  `fixtures`. Any refusal = «cancelled» (empty path, nil error) plus an error log line, and the line is consumed.
- `input/wails`: `Options.Hooks` (`Open`, `Save`, `Clipboard`, `Links`, `Opener`), routed in `Shell.dialogs()` and `OpenServices`; no adapter import
  (ARCH-40); nil = today's native behaviour. A reflection test shows no bound service exposes the hooks (the page cannot name a dialog or path).
- `app`: the recorder is built only in `e2e_hooks_on.go` (`//go:build e2e`); `e2e_hooks_off.go` returns nil. `coreEnv` also gives the library's
  `Calls` («Подключиться») the recorder in E2E mode (`env.calls`; production keeps `weblink.New()`, `testEnv` has none).
- Shipped exe: `go list -deps` of `./desktop/cmd/desktop` lists `adapters/e2ehooks` only with tag `e2e` (checked for tags "", `production`,
  `e2e`, `production,e2e`, host and GOOS=windows). So the `-tags production` exe does not contain the hooks.

## Scope deviations (accepted by the coordinator as routine)
`app/app.go`, `app/env.go`, `app/core.go` got small edits (field, `opts.Hooks`, `calls` env field, `Calls: e.calls`): ARCH-50 keeps wiring in `app`.

## Decisions to confirm
- «Подключиться» is hooked (kind `call`), Google's consent page stays native (ADR-0035 keeps Google out of E2E).
- Page-side `navigator.clipboard.writeText` (AgentCards, AgentPanel, MessageActions, agentView, OwnNotesParts) is NOT hooked: TASK-375 runner must
  stub it in the page (e.g. `addInitScript`) or deny clipboard permission; noted in `doc.go`.
- The runner must append picks lines with a trailing newline.

## Follow-ups (lows, not fixed)
- Hard links inside `fixtures/` / `saved/` are not detected (needs syscall; the runner is the only writer of the control dir).
- CI guard against a future untagged import of `e2ehooks` (e.g. `go list -deps -tags production ./desktop/cmd/desktop` must not list it).
- `safeName` comment says «at most 120 runes»; a device-name `_` prefix can make it 121.
- Tests of the E2E wiring run only with `-tags e2e` (CI runs that variant); untagged ones cover the off path.

## Verification
`go vet` and `go test -race -count=1` of `adapters/e2ehooks`, `input/wails`, `app` with and without `-tags e2e`; `GOOS=windows go vet [-tags e2e] ./desktop/...`;
`go test ./desktop/tests/...`; golangci-lint v2.14.0 on the three packages (default and `--build-tags=e2e`): 0 issues. Junction tests (real `mklink /J`) ran
and passed; symlink tests skip here (no privilege), run on Linux CI. `verify.sh` not run.

## Review
reviewer: approve, one medium (half-written picks line) fixed, lows fixed except those above; re-review of the fix commit: approve.
security-auditor: no high/medium; optional low (tag-gating) done.

## Changed existing tests
None. (Within the branch: `TestSafeName` long-name case replaced by `TestSafeNameLong`; `TestE2EHooks` adapted to the new signature.)

## Security line
Hooks reachable only in a `-tags e2e` build with `--e2e-dir`; not linked into the production exe; picks confined to `<e2e-dir>/fixtures` (links, junctions,
traversal, Windows names refused); saves confined to `<e2e-dir>/saved`; opener/clipboard only record; the page has no binding to the hooks.
