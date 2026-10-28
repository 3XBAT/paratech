# TASK-233 — panel v8 / catalog 8: ask the subject

## Done
- `hub/pkg/prompts/agent/panel/v8/system.md` = v7 + rule "Subject not named": no subject in the thread and none named -> `list_subjects`, then `ask_questions` "К какому предмету относится задание?" with subject names and «Без предмета» last, then `create_assignment` (no `subject` for «Без предмета»). A named subject or a thread subject is never asked; no date -> no question, no deadline. One v7 line reworded (it contradicted the rule).
- `v8/tools.json` and `shared/testdata/tool_catalog_v8.json` are identical; they equal v7 except the `catalog_version` line (the file carries its own version, same as v4 -> v5). TASK-234 must compare "v8 = v7 except the version line".
- Tests: `TestPanelV8Tools`, `TestPanelV8Prompt`, `TestToolCatalogV7IsFrozen`, eval `TestAgentEvalHubV8AskForTheSubject` (5 cases) and `TestAgentEvalHubV8SubjectQuestionShape`. v1..v7 still served.
- Verified: go test -race hub/... shared/..., lint 0 issues, buf lint, buf breaking clean. Postgres tests need a DSN (CI).

## Decisions to confirm
1. At most 5 subjects in the question (Hub choice allows 2-6 options), then «Без предмета»; not in the ADR.
2. No subjects at all -> no question (a single-option choice is invalid), create without subject.
3. Prompt says "no default", but the desktop parser (values/plan.go) preselects option 0, so the first subject is preselected in the UI. Owner may want the default on «Без предмета» (would need a prompt v9 or a pre-release edit of v8).

## Follow-ups
- Reviewer minor wording: "one choice question" vs "date question in the same ask_questions"; "five closest to the request" is vague when nothing is named. Not fixed (v8 text pinned by tests; fix before release if wanted).
- Eval gaps: no-subjects case and combined subject + date question are not covered. Level-1 evals do not prove model behaviour; live check is in TASK-234 agenteval.
- Deploy: Hub with v8 before the desktop on catalog 8 (TASK-234).

## Changed existing tests
`TestEmbeddedPanel` (8 versions); shared `toolCatalogVersion/File` -> 8; `TestToolCatalogV6IsFrozen` compares with the v7 file (same check).

## Security
Auditor: no findings. Tools unchanged in capability; create_assignment stays reversible with ID provenance check.
