# TASK-085 — Hub: Anthropic gateways (Bearer key, base URL with a path)

## Done
- Checked against master first: nothing of this was implemented (TASK-340 only added StepStream; all requests already go through one `post`).
- Adapter `hub/internal/adapters/anthropic`: `Auth` type (`x-api-key` default, `bearer`), `Options.Auth`; one `authorize` in `post` sets exactly one key header for Complete, Step, Stream, StepStream and the probe. An unknown mode is `ErrInvalidOptions`. `messagesURL` accepts a base path (`https://gw.example.ru/anthropic` -> `.../anthropic/v1/messages`), one trailing slash dropped, endpoint rebuilt from checked pieces. Rejected: percent-escapes, empty/`.`/`..` segments, characters outside `A-Za-z0-9-._~`, paths over 256 chars; query, fragment, user info and redirects still refused. String/GoString/LogValue show the mode (an unknown value prints `(invalid)`, never the key).
- Config: `HUB_ANTHROPIC_AUTH` / `-anthropic-auth` (flag wins), default `x-api-key`, checked only for serve; the error names the variable and the allowed values and does not quote the value. `validateBaseURL` accepts a path by the same rules (http still only to loopback). String/LogValue include the mode. `app.go` converts the string (config is std-only, ARCH-52).
- Docs: `docs/refs/anthropic.md` «Gateway», `paratech/README.md` (table row + Bearer example), ADR-0010 bullet «amended by TASK-085», adapter `doc.go`. Probe test: optional `PARATECH_PROBE_ANTHROPIC_AUTH`.
- Tests: `gateway_test.go` (all modes x 4 call kinds: exactly one key header, no key in errors; paths; same-host redirect refused in bearer mode; key not printed), `client_test.go` (accepted/rejected options and paths), `config/llm_test.go`, `app/llm_test.go` (wiring for both modes).

## Decisions to confirm
- Auth values are case-sensitive: `Bearer` is a startup error.
- A base URL ending in `/v1` is not special-cased (gives `/v1/v1/messages`); the docs say to give the path without `/v1`, as Claude Code does.
- The path rules exist in both config and adapter (defence in depth).

## Follow-ups (lows)
- Reject or warn on a base URL whose last segment is `v1`.
- Share one path-rule test table between config and adapter.
- `Client.String`/`LogValue` print the (already validated) mode raw instead of via `printable()`.
- `paratech-hub -h` prints flag defaults taken from the environment, so a key pasted by mistake into `HUB_ANTHROPIC_AUTH` would show there (same as a base URL with user info today); a `flag.Func` would fix it.
- `Options.String` prints `BaseURL` as given (not printed anywhere today).

## Changed existing tests
- `client_test.go` TestNewValidatesOptions: the case «path is rejected» removed (paths are allowed now), replaced by stricter path cases.
- `config/llm_test.go`: the «base URL with path» error case removed for the same reason; the settings/flag tests gained checks for the new setting.
- `config/config_test.go` TestLoad: expected `Config` gets `AnthropicAuth: "x-api-key"` (whole-struct comparison, new default).

## Security
Auditor: no high/medium; checked key leaks in both modes (String/LogValue/errors/misplaced key), redirects, path tricks, header injection, loopback rule, one key header only. Low: `-h` default echo (above). Reviewer: approve, lows above.
