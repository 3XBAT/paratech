# TASK-200 — check steps

Hub only; the desktop still sends catalog 4, so panel prompt v5 is not active until a desktop task bumps it.

1. Restart the Hub with the new build. In the panel ask «что такое горутина». Expected: a reply card with the short answer, not «Не получилось выполнить шаг». Hub log shows `text_reply=true` (or, for a long answer, `reason=no_tool_use`).
2. Triage an assignment that is only partly doable. Expected: no `provider_error ... needs a helper`; the log may show `reask=accepted`. A refusal shows `reask=refused` and `reason=invalid_answer`.
3. Hub logs of failed steps carry a `reason`, so they do not read like a lost connection.
