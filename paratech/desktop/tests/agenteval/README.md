# Agent live check (level 2)

Case set `testdata/live_cases.json`: 40–60 student requests (expected plan class, optional `also` classes the prompt allows too, and tools in order), 5 «cannot do
whole» cases (→ `cannot_do`), 20 lecture questions (→ `answer_question`, chunks and a reference citation). All data
is fake; addresses only `example.invalid`.

Metrics: plan class and tool routing, `cannot_do` share, share of card texts with first person (я/мы/I/we) and with
an address or greeting (ты/вы/привет), replies that list the assignments found (`lists_assignments`, catalog v3),
share of valid citations (not measured until the Hub has the ANSWER prompt, TASK-118). Cases `b01`–`b05` are the beta
questions of TASK-172/173 (greeting, «где черновики», «что горит», «помоги с пр 2-4», a topic without an assignment).

CI (no tag) runs the scoring tests, checks the case file and runs the runner on the fake Hub.

## Run (never in CI; spends the tester key's quota)

PowerShell, from `paratech/`:

```
$env:PARATECH_AGENT_LIVE = "1"
$env:PARATECH_AGENT_LIVE_HUB_URL = "https://<hub>"      # no default
$env:PARATECH_AGENT_LIVE_KEY = "<tester key>"           # never commit it
$env:PARATECH_AGENT_LIVE_REPORT = "C:\temp\agent-live.md" # optional
$env:PARATECH_AGENT_LIVE_ONLY = "r01,w01"               # optional subset
go test -tags agentlive -run TestAgentLive -count=1 -timeout 60m -v ./desktop/tests/agenteval/
```

Read tools get canned results; a run stops at the first card or action, nothing is executed. Attach the report to
every PR that changes the agent prompt or the catalog.

## Extending

Add cases in every phase: new tools, new failure seen live, new «cannot do» portals. `TestCaseSetFile` keeps the
file consistent with the golden catalog.
