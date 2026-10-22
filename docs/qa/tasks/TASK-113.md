# TASK-113: check steps

No user-visible change. Contract only.

1. From `paratech/`: `go test -race ./shared/... ./hub/internal/input/connect/...` is green.
2. In `paratech/shared`: `buf lint` and `buf breaking --against '.git#branch=master'` are green.
3. Hub: calling `AgentStep` with a valid key returns `unimplemented`; without a key `unauthenticated`.
