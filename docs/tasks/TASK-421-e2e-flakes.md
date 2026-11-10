# TASK-421..423: E2E flakes — likely causes (read from the code, 10.11)

## TASK-421: `specs/agent/08-answer.spec.ts:129` (row «что было на паре — карточка с источником»)
- Seen: progress/TASK-401.md, second full run; first run green on the same build.
- Cause: every import of the recording pack uses the same clip, so the same summary text. `02a-retranscribe` adds
  «Человек в футляре · значок отмены» with the summary that also has «калоши». The `cited` rule cites the FIRST
  fragment of the ANSWER request; equally ranked fragments come in no fixed order, so the source may be that
  recording, and line 129 accepts only «основная» and «перезапуск».
- Fix: accept any recording of the recording pack that exists in the app (`recordingsOfApp`) and whose title is one of
  the pack's lectures (`lectureOf(...)` of every import, incl. «значок отмены»); keep every other assertion (one ANSWER
  request, fragments with «калоши», counters only, the tab of the place). Do not weaken to «any page».

## TASK-422: `test/layout-rules.test.ts`, CI job `e2e-static` (ubuntu, headless Edge is installed there)
- Seen: supervisor-decisions 09.11, «evalInPage under the CSP of the app» / «a running finite animation is waited for
  without a console error».
- Causes:
  1. Animation test (line 304): `started` is taken AFTER `evalInPage` returned, i.e. after the 500 ms animation began;
     on a slow runner more than 250 ms of it is already gone, so `Date.now() - started >= 250` fails although the
     check waited correctly. Fix: assert the state, not the clock — after `layoutViolations` returns,
     `document.getAnimations().length === 0` (via `evalInPage`), and start a longer animation (2 s) so the wait is
     observable; no wall-clock lower bound.
  2. «the reason evalInPage exists» (line 270): expects the CSP console record of `waitForFunction` polling within a
     600 ms timeout + 300 ms; on a slow runner the polls may not happen in time. Fix: poll `drainConsole` until the
     record appears (deadline 5 s), keep the assertion.
  3. «a value comes back…» (line 240) asserts an EMPTY console without filtering `kind: "request"` (the clipboard test
     does filter): a stray request record (favicon or a late request) fails it. Fix: filter `request` records as the
     clipboard test does, or route them; the CSP assertion itself stays.
- First step: `gh -R 3XBAT/paratech run view <id> --log-failed` of the failed runs (supervisor-decisions 09.11) to see
  which assertion fired; fix every one above anyway. Prove: 20 runs in a row of the file locally (`node --test` with a
  loop script in the scratchpad), all green.

## TASK-423: `test/hub.integration.test.ts` hangs (>13 min, progress/TASK-401.md; three node processes left)
- Likely causes, in order:
  1. `src/hub/proc.ts` `run()` resolves only on `close` (all stdio pipes closed). On Windows a grandchild of
     `docker compose` or `go build` that inherited the pipes keeps them open after the child exits, and after a timeout
     `killTree` may not reach a re-parented grandchild, so the promise never settles. Fix: settle on `exit`, then give
     the pipes a short grace (e.g. 2 s) and destroy them; a timeout always settles.
  2. Every `fetch` of the test has no deadline (`connectJSON`, `connectStream`); a Hub that accepts and never answers
     (cut stream case, Postgres stalled) holds the test until the 900 s test timeout. Fix:
     `AbortSignal.timeout(30_000)` on each call.
  3. Open handles after the test (undici keep-alive pool, a child not unref'd) keep `node --test` alive. Fix: after
     `hub.stop()`/`fake.stop()` log `process.getActiveResourcesInfo()` as a diagnostic and make the test file end.
- Add per-phase `t.diagnostic` with elapsed ms (build, compose up, schema, key, ready, each call, stop) so the next hang
  names its phase. Run it twice on this machine with Docker up (the owner starts Docker; never start it); no leftover
  `node`, `hub.exe`, compose project after each run.
