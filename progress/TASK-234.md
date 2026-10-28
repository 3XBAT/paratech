# TASK-234 — desktop on catalog 8 (subject question)

## Done
- `values.CatalogVersion = 8`. Golden test now points at shared/testdata/tool_catalog_v8.json; `TestToolCatalogV8KeepsTheToolsOfV7` pins that v8 equals v7 byte for byte except the `catalog_version` line.
- Subject question of panel prompt v8 («К какому предмету относится задание?», last option «Без предмета», `values.SubjectQuestion` / `NoSubjectOption`): the parser gives it `Default = values.NoDefault` (-1), nothing preselected. A planner `default` on this question is ignored (untrusted LLM output); a negative or out-of-range planner default is still refused. Other choice questions keep the old rule (no default = option 0).
- `TestPanelPromptV8PinsTheSubjectQuestion` ties both strings to hub/pkg/prompts/agent/panel/v8/system.md (reading the Hub file; a reworded prompt fails the test instead of silently preselecting option 0).
- Frontend (question card only): `collectAnswers` returns null while a default -1 choice is unpicked (submit disabled); «Пропустить» picks the last option («Без предмета») for such a choice. AgentCards.tsx needed no change (no radio matches -1).
- agenteval: CatalogFile v8, `answer` / `assigned_subject` case fields, cases w12, w13 (answer «Без предмета»), s01–s04 of TASK-233, scoring and runner updated; README.
- Integration test `TestAgentAsksTheSubjectOfAnAssignment` (a subject / no subject, -1 and MaxOptions refused, card keeps waiting). `TestAgentRequestOfThePreviousCatalogAfterTheUpdate` now covers 7 -> 8 (old request: «Продолжить»/«Повторить» honest refusal, new request works).

## Deployment order
Hub with catalog 8 / prompt v8 first, then the desktop build (a desktop on 8 against a Hub without 8 would be refused as with earlier catalogs).

## Decisions to confirm
- «Пропустить — сделай как считаешь» on the subject question files the assignment without a subject (last option), not under the first subject.
- Planner default on the subject question is ignored entirely (not even «Без предмета» may be preselected).

## Follow-ups
- Answers are not compared with the open card: `values.AnswersResult` only checks 0 <= option < MaxOptions (6), so a crafted binding call could send option 2..5 on a 2-option card or omit the subject question. The UI cannot produce it; no side effect (the answer goes to the planner only). A card-aware `CheckAnswers` in agentturn.AnswerQuestions would close it.
- agenteval `AssignedSubject` treats `"subject":""` as a miss, not as «none» (README could say so).
- Live agenteval of prompt v8 (needs a key).

## Changed existing tests
- values/args_test.go `TestSubjectQuestionNoDefault`: the «explicit default wins» rows moved from the subject question to a non-subject question (the planner default is now ignored on the subject question); tool_test / result_test: catalog version 8; agent_catalog_test, agenteval score/runner/cases tests updated for v8 and the new fields.

## Security
Reviewer: two mediums (planner default could preselect the first subject: fixed; answers not checked against the card: pre-existing, follow-up) and two lows (prompt coupling: test added; AssignedSubject edge: noted). No re-review (small tested fix). Auditor: no findings; create_assignment stays a reversible U-class action with Core-side subject-ID validation.
