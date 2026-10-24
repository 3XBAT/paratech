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

## Addendum (TASK-131): writing the work
- **Job `work_writing`** (hidden from «Записи и задачи»): the agent tool `write_work` (class U, paid) always runs on
  an approved plan, only for a confirmed work. The planner gets the work ID and enums, never a text.
- **Steps**: outline first, then one section per queue step (one Hub `Complete` call each, key = work, revision,
  step, attempt). A repeated step reads the stored section and asks nothing, so a crash or retry never pays twice.
  Editing the outline or regenerating a section rewrites only what changed.
- **The student approves the outline**: the job ends once the outline is stored (no attempt used while it waits).
  Only the student, through `input/wails`, edits it and presses «Утвердить план» (`ApproveOutline`), which starts
  the sections job; a regeneration is the student's own request and approves it too. The agent's port has none of
  these moves, and its `write_work` on a work whose outline waits is `not_now`.
- **Untrusted text**: the brief (card, questions and answers, the reader's assumptions) goes to the writer as one
  JSON data field inside the delimited data block, never into the instructions; section texts are checked again
  and are paper text only.
- **Result**: a GOST `.docx` filed into the subject's ДЗ through the library port; Undo of `write_work` moves the
  paper back to «Входящие», the file kept — only the paper of its own generation, untouched by the student. When a
  new generation (an edit or a regeneration of a done work) files its paper, the previous paper leaves ДЗ for
  «Входящие» the same way, as does a paper filed by a step whose outline changed meanwhile. The writing is stored in
  `works.writing` (migration 0027) as canonical JSON read back through the strict parser.
- **Deleting the assignment** cancels the work and its writing job.

## Consequences
- Answers, edits and confirmation come only from `input/wails`; the agent can start a triage, nothing more.
- The triage outcome of a request is not fed to the plan policy yet (`Facts.Triage`); the mandatory plan covers it.
- «Спросить по шагу» needs a Hub operation (a later task).
