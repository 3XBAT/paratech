# TASK-085 — Hub behind a Bearer gateway (check steps)

No UI change: this is about how the Hub talks to the LLM provider. Do not paste the real key anywhere except the environment of the Hub process.

1. Run the automated check: `go test -race -count=1 ./hub/...` in `paratech/`.
2. Start the Hub as before (default): nothing changes, the key goes in `x-api-key` to https://api.anthropic.com.
3. Start the Hub for the gateway: set `HUB_ANTHROPIC_BASE_URL=https://ai.starimg.ru` and `HUB_ANTHROPIC_AUTH=bearer` (plus your `HUB_ANTHROPIC_API_KEY`). If the gateway lives under a path, put it in the URL without `/v1`, e.g. `https://gw.example.ru/anthropic`.
4. In the desktop app, record a short class (or use an existing recording) and get the summary: it must be produced; the agent panel must answer too.
5. Negative checks: `HUB_ANTHROPIC_AUTH=foo` makes the Hub refuse to start with `HUB_ANTHROPIC_AUTH: unknown value, want x-api-key or bearer` (the value is not echoed); a base URL with `?x=1`, `user@` or `http://` to a non-local host is refused with a message naming `HUB_ANTHROPIC_BASE_URL`.
6. Hub logs and `paratech-hub` startup output show `anthropic_auth=bearer` but never the key.
