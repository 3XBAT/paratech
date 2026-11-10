# Agent live check (level 2)

Case set `testdata/live_cases.json`: 40–80 student requests (expected plan class, optional `also` classes the prompt allows too, and tools in order), 5 «cannot do
whole» cases (→ `cannot_do`), 20 lecture questions (→ `answer_question`, chunks and a reference citation). All data
is fake; addresses only `example.invalid`.

Metrics: plan class and tool routing, `cannot_do` share, share of card texts with first person (я/мы/I/we) and with
an address or greeting (ты/вы/привет), replies that list the assignments found (`lists_assignments`, catalog v3),
`triage_assignment` with the expected source (`source`: assignment, upload or topic; catalog v4), share of valid
citations (not measured until the Hub has the ANSWER prompt, TASK-118). Cases `b01`–`b05` are the beta questions of
TASK-172/173 (greeting, «где черновики», «что горит», «помоги с пр 2-4», a topic without an assignment, which goes
to `triage_assignment{topic}` since catalog v4). Cases `w12`, `w13` are the owner's requests that add an assignment
(`create_assignment`, catalog v6). Since catalog v7 a reply text may have up to 4000 characters (TASK-223). Since
catalog v8 a request without a subject in a thread without one is asked «К какому предмету относится задание?»
(TASK-233): a case with `answer` picks that option on the first question (the run sends its index and goes on), and
`assigned_subject` is the subject `create_assignment` should get (`none` after «Без предмета»); cases `w13`,
`s01`–`s04`. Since catalog v9 (chats, TASK-244/245) a case may carry `earlier` requests (`text`, `reply`, sent as
pairs before its text), a `summary` (the first history turn) and `mentions` (`kind`, `id`, `earlier: true` for a
mention of an earlier request; IDs only, never labels): `m01` a mention of another subject's lecture, `m02` «лекция 5»
without a mention (look it up, never invent an ID), `m03` a summary with an instruction that must not be obeyed, `m04`
an earlier mention that is not the subject of the request. Since TASK-366 a case may carry `results` (its own result of
a read tool instead of the canned one), `avoid` (phrases no card text may contain) and `subject_name` (the name
`create_subject` should get): `f01` a failed `answer_question` gets a restrained reply without a cause (whether it
offers to retry is read in the report), `f02` «добавь предмет по последней записи» looks the recordings up and adds
the subject of the newest one without asking. Since catalog v10 (wave 10, TASK-326) a case may carry `args` (per
tool, the argument values its first call gives, strings verbatim; `{}` = no argument), `once` (expected tools called
exactly once) and `never` (tools no call may reach: an instruction inside pasted data is data): `v01` schedule →
`propose_subjects_from_schedule {}`, `v02` one `mark_assignments_done` after `list_assignments`, `v03` the note text
verbatim, `v04` `set_setting`, `v05` `recording{start}`, `v06` `offer_button`, `v07` `change_assignment{delete}` (the
desktop makes it the `delete_assignment` card when the assignment has a Google event; checked in
`desktop/tests/agent_catalog_test.go`), `v08` twelve writes → `show_plan`, `v09`/`v10` an injected instruction in a
pasted schedule and in a note text. Under the v10 «fewer «Да»» rules up to six reversible writes run without a plan,
so `p01`, `p03` expect `write` (a plan still counts) and `p06` accepts `write`. The run is scored against
`shared/testdata/tool_catalog_v10.json`.

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
