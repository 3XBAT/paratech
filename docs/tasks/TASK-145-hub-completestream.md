# TASK-145: Hub `CompleteStream` for the ANSWER reader

- **Agent:** hub-dev (opus)
- **Label:** hub
- **Depends on:** TASK-144
- **PRD sections / AC:** §4.4, §9.2, §13.2; ADR-0005(d), ADR-0008, ADR-0023, ADR-0026 «Streaming»
- **Branch:** ralph/<date>-task-145

## Goal
`GatewayService.CompleteStream` serves `OPERATION_TYPE_ANSWER` with the ledger rules of `Complete`, settling every
call exactly once, with a config flag that falls back to a non-streaming provider call.

## Allowed to change
- `paratech/shared/proto/paratech/hub/v1/gateway.proto` (comments only: cancel semantics, ANSWER only)
- `paratech/hub/internal/cases/gateway/*` (port `StreamingProvider`, `CompleteStream`) (+ tests)
- `paratech/hub/internal/input/connect/{gateway.go,gateway_test.go,auth*_test.go,router_test.go}`
- `paratech/hub/internal/app/*`, `paratech/hub/config/*` (flag `provider_stream`, default false, named in the style
  of the existing settings)
- `paratech/hub/testenv/{provider.go,messagesapi.go}` (scripted streaming fake), `paratech/hub/tests/keys_test.go`,
  `paratech/README.md` (one line)

## Out of scope
- Desktop (TASK-146), `AgentStep` streaming, partial settlement.

## Acceptance criteria
- [ ] Proto comments: a client cancel lets the provider finish (call timeout), settles in full and stores the result,
      as `Complete`; only `OPERATION_TYPE_ANSWER` (others `invalid_argument`); `buf lint` and `buf breaking` green.
- [ ] Same validation, server prompt, fingerprint and key life cycle as `Complete`; `Complete` and `CompleteStream`
      with the same key and spec replay each other's stored result; a replay is one delta then the result.
- [ ] Provider call on a context detached from the client; client gone mid-stream → the Hub reads to the end, one
      full settle, result stored; a later call with the key replays free (test with a cancelled client).
- [ ] Provider failure before completion → `PROVIDER_ERROR`, reservation released; ANSWER schema check on the whole
      output with the charged failure marker rule of ADR-0023; no delta is sent after the error.
- [ ] `provider_stream=false`: `Provider.Complete`, whole output as one delta. `true`: `StreamingProvider.Stream`.
- [ ] The Hub logs no texts; `TestCompleteStreamIsUnimplemented` and similar tests replaced by real behaviour tests.
- [ ] `go vet`, `go test -race ./hub/...` (Postgres tests as in CI), `golangci-lint`, `buf` checks pass.
