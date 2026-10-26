# TASK-210 — desktop on tool catalog 5

## Done
- `values.CatalogVersion = 5`; golden reference and agenteval `CatalogFile` moved to `shared/testdata/tool_catalog_v5.json`.
- `TestToolCatalogV5KeepsTheToolsOfV4`: v4 and v5 golden files are byte-identical except the `catalog_version` line.
- `TestAgentRequestOfCatalog4AfterTheUpdateTo5`: a call sealed under catalog 4, then the app sends 5: «Продолжить» and «Повторить» end with the honest refusal (StopFailed), no extra tool run, a new request works. Same behaviour as TASK-184; no production code changed beyond the constant.
- Test fake Hub (`testenv/agenthub.go`): seal now includes the catalog version like the real Hub; `AsCatalog(v)` simulates a desktop on an older catalog.

## Existing tests changed
- `values/tool_test.go`: expects 5. `TestToolCatalogMatchesGolden`: loads the golden file by `CatalogVersion` (logic unchanged).

## Decisions to confirm
- The new migration test is a fixed 4 -> 5 pin; the next catalog bump needs its own test.

## Follow-ups
- Live agenteval against the Hub with v5 was not run (needs a key).

## Security
Reviewer: approve (two mediums fixed/recorded here). Security-auditor: no findings; the catalog is unchanged, old seals fail closed.
