# TASK-144: how to run the gateway streaming probe

The probe makes ONE short streamed call (about 60 numbers in words, max 400 tokens) through your gateway. It is
skipped unless the env vars below are set, so CI and normal test runs never touch the network.

1. Pick the key you use for the gateway (ai.starimg.ru). Do not paste it into any file or chat.
2. In PowerShell, from the repository root of this branch:

```
$env:PARATECH_PROBE_ANTHROPIC_URL = "https://ai.starimg.ru"
$env:PARATECH_PROBE_ANTHROPIC_KEY = "<your gateway key>"
$env:PARATECH_PROBE_ANTHROPIC_MODEL = "<model id the gateway serves>"   # optional, default claude-haiku-5-5
go -C paratech test -count=1 -run TestProbeGatewayStreaming -v ./hub/internal/adapters/anthropic/
```

URL is the base address only (no path, no query).

3. Read the result.
   - PASS: the log shows HTTP status, content type, event names with arrival times, and the delta count with
     first/last/end timings. At least 2 deltas arrived and the first one came 100 ms or more before the end, so
     the gateway streams. `provider_stream` may be turned on later (TASK-145).
   - FAIL with "unexpected content type", fewer than 2 deltas, or the first delta right at the end: the gateway
     buffers or ignores `stream: true`. `provider_stream` stays off (fallback in TASK-145).
4. The log never contains the key or any text. Clear the variables afterwards (`Remove-Item Env:PARATECH_PROBE_*`).
5. Send me the PASS/FAIL and the timing lines; I record them in progress/TASK-144.md.
