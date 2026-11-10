# TASK-422 — check steps (no app UI; the e2e runner's own unit tests)

Nothing to see in the app: the app does not start for this check.

1. `npm --prefix paratech/e2e test` — all tests pass (450), none fail.
2. Run `node --test paratech/e2e/build/test/fakellm-stream.test.js` several times in a row (one after another): always green.
3. Same for `paratech/e2e/build/test/layout-rules.test.js` (needs the installed Edge; without it the browser tests are skipped).
4. On the PR the `e2e-static` job is green; if it ever fails again, the message names the state it waited for (for example «timed out after 5000 ms waiting for: the request reaches the fake»), not a time bound.
