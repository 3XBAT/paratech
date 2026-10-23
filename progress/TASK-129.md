# TASK-129: turnkey work aggregate, triage_assignment, questions <= 2 rounds, helper mode

Status: done (PR open).

## Done
- Hub: TRIAGE_ASSIGNMENT reader (prompt ops/triage_assignment/v1, strict schema, strong alias), rounds 1-2 may ask, round 3 is final and its questions are cleared; strictjson extracted from inspect; planner prompt describes list_assignments `version`.
- Desktop: domain/coursework/work aggregate, migration 0026 (works, work_rounds), cases/turnkey, hubclient TriageAssignment, agent tool triage_assignment + remove_work undo, work card, Wails TurnkeyService, e2e tests, injection fixture, ADR-0025.
- Review fix: in round 3 the desktop now drops the questions like the Hub (was: refuse + paid retry).
- Verified: go vet desktop/hub/shared, GOOS=windows vet, go test -race of touched scope and importers, desktop/tests, golangci-lint desktop 0 issues, sqlc no drift. After merging master: input, app, tests re-run green. verify.sh not run.

## Decisions to confirm
- Two real question rounds; round 3 = final, questions cleared on Hub and desktop, request continues with assumptions; strong alias per ADR-0023.
- Planner prompt v1 edited in place (version field), not v2 (prompts/doc.go says new version dir): supervisor to accept the exception or ask for agent/panel/v2.
- Extra status `review` between triage and confirmed; impossible goes straight to helper even with questions.
- triage_assignment is a paid write: counts toward the paid-job limit, needs an approved plan.
- Instruction channel (TASK-094) = `instruction` argument of Drafts.Generate; work.Instruction() builds it after confirmation (writer is TASK-105).
- Undo of triage removes the work while the student has not answered/edited; no refund.
- Prompt path is ops/triage_assignment/v1 (matches the operation enum), not ops/triage/v1 from the AC.

## Follow-ups
- AC gap (reviewer High): «Спросить по шагу» = narrow-context thread has no backend (no Hub op; StepAdvisor port unimplemented); binding answers «Вопросы по шагу пока недоступны.» Needs a follow-up task (TASK-130 consumes AskStep).
- Security Low: triage texts are not checked for URLs/emails/paths (only the prompt forbids); TASK-130 must render plain text only. Work.Instruction() carries reader-written assumptions/questions into the instruction channel: TASK-105 should pass them as data or mark reader-derived.
- Review Low: planner cannot tell why a triage did not finish (all map to `pending`); no golden test that Hub and desktop strict parsers agree (shared/testdata/triage); dropped round-3 questions could become assumptions; undo racing Retry loses a charge (rare).
- Facts.Triage not reported; planner prompt does not describe triage result fields.
- UI (TASK-130) uses TurnkeyService (Work, WorkOfAssignment, AnswerQuestions, SkipQuestions, EditUnderstanding, ConfirmRequirements, Retry, AskStep); panel card kind `work`.

## Changed existing tests
sqlite/migrate_test.go, tests/schema_test.go, sqlite/agent_test.go (ids via Sprintf), shared/agent_ids_test.go, random_test.go, agent/values_test.go, wails/agent_test.go, shell_test.go, settings_test.go, hub prompts_test.go, hub/tests/agent_eval_test.go; review fix: turnkey/service_test.go (round 3 refusal -> dropped), work_test.go, triage_test.go.

## Security
security-auditor: no High/Medium; two Low (above). Hard rule 3: model output is strictly re-validated data on Hub and desktop, no side effects; planner sees only enums and counts.
