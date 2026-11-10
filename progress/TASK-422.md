# TASK-422 — e2e-static flakes (layout-rules, fakellm-stream)

## Done
Tests only (`paratech/e2e/test/`); no `src/checks` or `src/fakellm` change — the bug was in the tests.

- `fakellm-stream.test.ts` (supervisor addition, PR #453/#456). Root cause: «JSON answer comes no earlier than delayMs»
  waited a fixed 60 ms and asserted `remaining() === 0`. On a slow runner the request had not reached the fake yet
  («consumed at once 1 !== 0»); the test threw with the request still in flight, the late request arrived after the next
  test's `setScript`, consumed its rule, and «the first byte of an SSE answer waits too» got an instant no-rule answer.
  Fix: poll `fake.requests().length === 1` (state), `finally settled(pending)` so no request outlives its test, an
  `answered === false` check keeps the «recorded and consumed before the wait» proof. The abort test waits for the
  request to be inside the wait before aborting. `chunkDelayMs` span is now measured from t0 (lower bound only), since a
  stalled client loop can batch deltas into one read.
- `layout-rules.test.ts`: «a value comes back…» filters `kind: "request"` like the clipboard test; «the reason
  evalInPage exists» accumulates `drainConsole` until the `script-src` record appears (5 s ceiling, waitForFunction
  timeout 5 s, polling ended by context close); animation test is state-based (animation running in the same evaluate,
  nothing `running` after `layoutViolations`), no `Date.now()` bound.

## Decisions to confirm
- Animation lasts 1500 ms, not 2 s as the task text says: the checker's cap `animationWaitMs` is 2000 ms and starts after
  the animation, so a 2 s animation could end together with the cap. The final assertion is
  `getAnimations().every(a => a.playState !== 'running')` instead of `length === 0` (the checker guarantees «nothing
  runs», not an empty list).

## Verification (all sequential, no artificial load)
- `npm run build` (tsc) clean; `npm --prefix paratech/e2e test`: 450 tests, 0 fail, 0 skipped (headless Edge present here).
- 30 sequential `node --test` runs each of `layout-rules.test.js` and `fakellm-stream.test.js`: 30/30 green (the
  fakellm file was rerun 30/30 after the review fix too).
- Slow runner imitated by a 150 ms delay injected in the test's `call` helper (temporary scratch test, removed): the OLD
  tests 1+2 fail with the CI symptoms («consumed at once 1 !== 0», «headers after 163 ms»), the NEW ones pass.

## Review / security
Reviewer: one medium (the before-the-wait proof weakened) — fixed; one low (chunkDelayMs span) — fixed; low (animation
deviation) recorded above. No re-review (small fix, verified by 30 runs). Security-auditor: no findings.

## Follow-ups
- On ubuntu CI the Edge tests of `layout-rules` probably skip (`skipReason`); the fakellm fixes are the ones CI exercises.
  Not verified against the CI runner.
- Remaining fixed sleeps (150 ms before an «absent record» check, 350 ms after the abort) are justified: they cannot
  cause a false failure.

Security: no new surface, test-only change.
