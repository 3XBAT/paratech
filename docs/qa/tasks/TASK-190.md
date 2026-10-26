# TASK-190 — owner's check

Streaming is now on by default; the probe step in `docs/qa/tasks/TASK-144.md` is no longer a precondition.

1. Start the Hub without `HUB_PROVIDER_STREAM`: a plain-text ANSWER streams (after the desktop chain TASK-146..148 lands).
2. If the gateway buffers (answers JSON), the Hub log has one line «provider answered without SSE» and the answer
   arrives as a single chunk; no second charge.
3. To switch off: `HUB_PROVIDER_STREAM=false` or `-provider-stream=false`; restart the Hub.
