# TASK-145: how to check Hub CompleteStream

No UI yet (the desktop side is TASK-146). Checks run on the Hub only, with fakes.

1. From the repository root of this branch:
   `go -C paratech test -race -count=1 ./hub/internal/cases/gateway/ ./hub/internal/input/connect/ ./hub/config/`
   Expected: ok. The tests cover flag off (one chunk) and on (raw chunks), replay across Complete and CompleteStream,
   a cancelled client (settled once, stored, free replay) and provider failures (reservation released, no text after
   the error).
2. `provider_stream` stays off by default. Do not enable it (`HUB_PROVIDER_STREAM=true`) on your running Hub until
   the TASK-144 gateway probe has passed.
3. Postgres tests run in CI (`HUB_TEST_DATABASE_URL`); locally they skip unless you point it at a throwaway database.
