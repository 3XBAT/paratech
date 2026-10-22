# TASK-135: agent checks (level 1 CI, level 2 live, injection fixtures)

Done: level 1 in `desktop/tests/agent_eval_test.go`, `agent_injection_test.go`, `agent_fixtures_test.go` (real agentturn + SQLite + hubclient + fake Hub) and `hub/tests/agent_eval_test.go` (real handler, seal, catalog, no Postgres). Fixtures are shared by desktop and Hub in `shared/testdata/agent/{replay,injection}` (README there). Level 2 in `desktop/tests/agenteval` (tag `agentlive` + `PARATECH_AGENT_LIVE=1` + URL/key env, no defaults; 45 requests, 20 lecture questions, 5 cannot_do; scoring helpers are unit-tested in CI).

## Bug found and fixed (B1, High)
Hub (`toolcatalog/schema.go`) takes plan steps as `{tool, arguments}` and questions as `{kind, question, ...}`; the desktop parser took `args` / `text`, so no plan or question card could work with the real Hub. Desktop `values/plan.go`, `args.go` now use the Hub shape. Limits: options 5 -> 6, text list 1..8 -> 0..10 (cannot_do needs at least one part), choice `default` optional, text question may have `options: []`. Max questions stays 3 (planner prompt says 3). Stored plans (`proposed_plan`, migration 0022) change key to `arguments`; the feature is unreleased, no compatibility.

## Decisions to confirm
- Fixtures live in `shared/testdata` (Hub tests read them too), not `desktop/tests/testdata`.
- Third question round refused; paid READ tools not counted in the "1 paid job" limit (supervisor).

## Follow-ups
- Pending driver (TASK-117/127): C tool not executed end to end through the loop; `input/agent` has no Confirm/Execute/Send/Delete (now only a reflection name check); result-mapper allowlist drops transcript/file text (ADR-0022); ICS lesson title through the real loop.
- TASK-122: reader output with free text in an enum field. TASK-118: live citation share is reported "not measured" until the Hub has the ANSWER prompt (AC "share of valid citations" is only partly done).
- Medium for TASK-117/121: `agentturn.drive()` stops the step if a base subject name fails `values.ParseName`; library names (schedule, up to 200 chars, e.g. with `:`) must be cleaned or dropped in the mapping, not fail.
- Low: Hub accepts `<`/`>` in card text, duplicate JSON keys (last wins), empty text_list, ID-shaped text; desktop is stricter (safe, a planner step is wasted).
- Low: `card:<kind>` replay expectation does not check the call id; Hub injection slot sweep has no per-fixture subtests; `score_test.go:159` keeps one old-shape `text` question (scorer does not parse calls).
- Plan-step arrays for `ids` arguments are still accepted by the desktop; the Hub only sends strings.
- Lint was not run by implementers (heavy.sh lock timeouts); left to CI.

## Changed existing tests
Only JSON key renames (`args`->`arguments`, `text`->`question`) in values/thread/agentturn/sqlite/`agent_sqlite_test.go`. In `values/args_test.go`: "no default" now accepted (Hub makes it optional), "text list too long" uses Max+1.
