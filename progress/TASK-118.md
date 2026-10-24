# TASK-118: answers with citations (Hub ANSWER + desktop answering, answer_question)

Status: done (PR open).

## Done
- Hub: prompts ops/answer/v1, OPERATION_TYPE_ANSWER via Complete on the strong alias (Sonnet), strict schema {claims[{text,cites[{chunk_id,quote}]}],missing[]}, bounds, unknown chunk ids refused, refusal charged once and replayed free. Catalog v2 (new dir hub/pkg/prompts/agent/panel/v2 + shared/testdata/tool_catalog_v2.json): new ArgType `query` (<=500, one line) for search.query and answer_question.question; v1 stays frozen and served.
- Desktop: catalog v2 mirror, domain/coursework/answer aggregate, services/citation (case, ё, spaces, dashes, invisible chars; min 8 chars; quote must occur in the cited fragment), KB passages (<=12 x 2 KiB, scan at query time, ADR-0019), cases/answering.Ask, hubclient AnswerQuestion, migration 0027 answers, answer_question tool in input/agent (paid READ, not counted in the paid-job limit), Driver emits AnswerCard, Wails AnswerService.Answer(id) (text + sources with recordingId/section/index/startMs). Planner sees only answer id and counts.
- Verified: go vet desktop/hub/shared, GOOS=windows vet, go test -race ./desktop/... and ./hub/... ./shared/..., golangci-lint 0 issues, sqlc no drift, buf generate/lint/breaking. Hub Postgres tests not run locally (CI). verify.sh not run.

## Decisions to confirm
- Catalog bump is a NEW version dir (v2), v1 untouched: Hub accepts catalog_version equal to the dir number and prompts/doc.go requires a new dir. Deploy the Hub before shipping this desktop (desktop sends catalog 2).
- Unknown chunk id in a cite refuses the whole (charged) answer.
- Claim without a valid cite is kept but marked unsupported (not «из ваших лекций»).
- Driver emits the AnswerCard itself after answer_question.
- Quota / no connection reported to the planner as generic `failed`; min quote length 8; 12 passages x 2 KiB.
- Plan-step arguments stay at 80 characters (paid reads need no plan).
- TASK-131 merge: its edits to v1 tools.json / tool_catalog_v1.json must be ported to v2 (TestToolCatalogV1IsFrozen will flag).

## Follow-ups
- Reviewer MEDIUM (deferred, touches deletion code outside this task): answers are not purged with a recording (ADR-0018 «everything made from it»); quotes and recording ids stay, no retention (D6 does not cover). Needs a small task: purge/redact answers by source recording, card handles a missing recording.
- Frontend AnswerCard (fetch by answerId, plain-text rendering of claims/quotes/missing, open source like a KnowledgeHit; frontend/src/bindings/answers.ts) — separate task.
- Answer reused by key keeps the first subject (key lacks subject). architecture.md §12 lacks the `answer` aggregate. Strict-reader bounds duplicated Hub/desktop: add a parity test. Paid-read worst case may exceed the D8 threshold (owner attention). Retire catalog v1 later.

## Changed existing tests
Hub: gateway/server_prompt_test.go (ANSWER is now a reader), prompts_test.go, toolcatalog_test.go (golden v2), shared/contract_test.go, hub/tests/agent_eval_test.go. Desktop: migrate_test.go (answers table, version 27), shell_test.go, settings_test.go, hubclient/agent_test.go (catalog 2), catalog/agenteval tests point to v2; agent_driver_test.go comment only.

## Security
security-auditor: no High/Medium. Reviewer: approve; one Medium deferred (above), lows listed. Hard rule 3 holds: model output is strictly re-validated text shown in a card, planner sees ids and counts.
