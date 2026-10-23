# ADR-0025: Turnkey work («ДЗ под ключ»): triage, questions, helper mode

- **Status:** Accepted
- **Date:** 2026-10-23
- **Source:** TASK-129 (absorbs TASK-103); owner 2026-10-20 «ДЗ под ключ»; ADR-0021, ADR-0022, ADR-0023.

## Context
The student wants a finished work in the right format, not model text. Before writing, the app must understand the
assignment (type, requirements, whether it can be done), ask at most two rounds of questions, and say honestly when
it cannot do the work whole — then help instead. Everything the model says about the assignment is untrusted.

## Decision
- **Aggregate `coursework/work`** (desktop), statuses as a table: `triage` → `questions` (rounds 1–2) → `triage` of
  the next round → `review` («Как я понял задание») → `confirmed` → `writing` → `done`; an impossible work → `helper`;
  a reader that refuses 3 attempts of a round → `failed`. No status leads to a third question round: round 3 is the
  final reader call: the Hub clears the questions of its answer and the desktop does the same, so the answer goes to
  `review` with its assumptions instead of a refusal and a paid retry. «Пропустить» = every question takes its default.
- **Reader `TRIAGE_ASSIGNMENT`** (Hub, `Complete`, server prompt and strong alias): the desktop sends the assignment
  text, the teacher's material and the previous questions with the student's answers as data; the answer is checked
  again on the desktop with the Hub's strict schema (exact keys, enums, bounded texts, default index in range, helper
  rules). A malformed answer uses an attempt; it never panics. Key: work, data with the round, attempt.
- **Storage** (migration 0026): `works` (status, round, attempt, the student's card edits per field) and
  `work_rounds` (the canonical answer JSON, read back through the same strict parser, and the resolved replies).
  Local data is the system of record.
- **Partial** works are written with placeholders the student fills (flag; TASK-107). **Helper mode**: steps, what to
  submit, pitfalls, ready parts, placeholders; «Спросить по шагу» gets only that step and the requirements. The Hub
  has no operation for it yet: the port `StepAdvisor` stays unwired and the binding says «пока недоступны».
- **Agent tool `triage_assignment`** (class U, paid): it counts as the paid job without a plan, and with no cost
  estimate it always runs on an approved plan (supervisor rule). The planner gets the work ID, an outcome enum, the
  feasibility enum and the number of questions — never a text (ADR-0022). The panel gets a work card (ID + outcome);
  its texts come from the binding `TurnkeyService`. Undo removes the work while the student has not touched it;
  the reader call is not refunded.
- **Student's channel** (TASK-094): after «Подтвердить» the work gives the confirmed card, the answers and the stated
  assumptions as one instruction text for the writing (TASK-105).

## Consequences
- Answers, edits and confirmation come only from `input/wails`; the agent can start a triage, nothing more.
- The triage outcome of a request is not fed to the plan policy yet (`Facts.Triage`); the mandatory plan covers it.
- «Спросить по шагу» needs a Hub operation (a later task).
