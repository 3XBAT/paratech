# TASK-129 (WIP, paused): turnkey work aggregate, triage_assignment, questions <= 2 rounds, helper mode

Status: implementation done and verified on the touched scope; review, security review, bookkeeping, push and PR NOT done (paused by supervisor).

## Done
- Hub (e583f10a, 37c24c90): TRIAGE_ASSIGNMENT reader (prompt ops/triage_assignment/v1, strict schema, strong alias), rounds 1-2 may ask, round 3 is final (questions cleared); planner prompt now describes list_assignments `version`.
- Desktop (5eea7dd1..3f9c45e6): domain/coursework/work aggregate, migration 0026 (works, work_rounds), cases/turnkey, hubclient TriageAssignment, agent tool triage_assignment + remove_work undo, work card, Wails TurnkeyService, e2e tests, injection fixture, ADR-0025.
- Verified by go-core: go vet desktop/hub/shared, GOOS=windows vet, go test -race all three, golangci-lint desktop 0 issues, sqlc no drift. verify.sh not run.

## Left (next step)
1. Fetch; if master moved, merge origin/master (renumber migration 0026 if taken: file, migrate_test, docs/contracts/schema.sql).
2. Run reviewer and security-auditor in parallel (opus), fix findings, re-review after high/blocker.
3. Write docs/qa/tasks/TASK-129.md, set tasks.json TASK-129 status (and TASK-103 stays blocked/absorbed), commit "ralph: TASK-129 done (...)".
4. Push, gh pr create with body-file, run .github/scripts/audit-pr.sh.

## Decisions to confirm
- Two real question rounds (Hub round 3 = final, no questions); strong model alias per ADR-0023.
- Planner prompt v1 edited in place (version field), not v2.
- Extra status `review` between triage and confirmed; impossible goes straight to helper even with questions.
- triage_assignment is a paid write: counts toward the one-paid-job-without-plan limit, always needs approved plan.
- Instruction channel (TASK-094) = `instruction` argument of Drafts.Generate; work.Instruction() builds the text after confirmation (writer is TASK-105).
- Undo of triage removes the work while the student has not answered/edited; no refund.

## Follow-ups
- «Спросить по шагу» has no backend (no Hub op; StepAdvisor port unimplemented); binding answers «Вопросы по шагу пока недоступны.»
- Facts.Triage not reported; planner prompt does not describe triage result fields.
- UI (TASK-130) uses TurnkeyService (Work, WorkOfAssignment, AnswerQuestions, SkipQuestions, EditUnderstanding, ConfirmRequirements, Retry, AskStep); panel card kind `work`.

## Changed existing tests
sqlite/migrate_test.go, tests/schema_test.go, sqlite/agent_test.go (ids via Sprintf), shared/agent_ids_test.go, random_test.go, agent/values_test.go, wails/agent_test.go, shell_test.go, settings_test.go, hub prompts_test.go, hub/tests/agent_eval_test.go.

## Security
Not yet reviewed by security-auditor. Hard rule 3 design: model output is strictly re-validated data on Hub and desktop, no side effects.
