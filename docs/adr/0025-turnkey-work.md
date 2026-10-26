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
  submit, pitfalls, ready parts, placeholders; «Спросить по шагу» gets only that step and the requirements (wired by
  the TASK-179 addendum).
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
  these moves, and its `write_work` on a work whose outline waits is `not_now`. The student's `EditOutline` of a
  done work counts as approval: the new generation is written without a new «Утвердить план».
- **Blocked outline**: an outline call that used its attempts is not a dead end: the student's `Regenerate(0, note)`
  (`input/wails` only) asks for the outline again with new attempts and keys (no free replay of a refused answer)
  and the note in the student's section; that outline waits for the approval again.
- **Agent's job**: the `write_work` job completes at the outline; an Undo of it after that is «target changed». The UI shows a completed outline job as «план готов, утвердите», and never shows
  `Writing.Material` as the current paper while a step is running.
- **Untrusted text**: the brief (card, questions and answers, the reader's assumptions) goes to the writer as one
  JSON data field inside the delimited data block, never into the instructions; section texts are checked again
  and are paper text only.
- **Result**: a GOST `.docx` filed into the subject's ДЗ through the library port; Undo of `write_work` moves the
  paper back to «Входящие», the file kept — only the paper of its own generation, untouched by the student. When a
  new generation (an edit or a regeneration of a done work) files its paper, the previous paper leaves ДЗ for
  «Входящие» the same way, as does a paper filed by a step whose outline changed meanwhile. The writing is stored in
  `works.writing` (migration 0028) as canonical JSON read back through the strict parser.
- **Deleting the assignment** cancels the work and its writing job.

## Addendum (TASK-133): exam tickets
- **Aggregate** `coursework/ticketset`: proposed → confirmed → answering → done | failed | cancelled.
- **Parser**: deterministic, on the desktop (no Hub operation): numbered lines («1.», «1)», «Билет 1»), at most 100
  tickets of at most 500 characters; the list is untrusted data, never an instruction.
- **Confirm** only by the student (`input/wails` `TicketService`; the agent has no port to it). A run whose estimate
  is over the cap needs the student's acceptance of the cost; the estimate and cap shown are stored with the set and
  a stored set is checked against them, never against the current constants. The estimate is typical, not a bound.
- **Coverage** per ticket from its answer: covered (every claim supported by a checked citation), partial,
  not_covered («нет в ваших лекциях»: no supported claim); a refused answer is failed for that ticket only.
- **Job `ticket_answers`**: one ticket per queue step (`answering.Ask` through a port of the tickets use case), a
  stored result is never asked again. No connection or quota waits; a permanent Hub error fails the job, and the set
  shows as stalled until the student confirms again.
- **Result**: a `.docx` filed into the subject's «Билеты»; Undo moves it back to «Входящие», the file kept, only while
  it is still there. Files dedupe by sha256: two sets with identical bytes share one material, so an Undo of one
  moves the file of both.
- **Agent tool** `prepare_tickets` is deferred: it needs Hub tool catalog v3.

## Addendum (TASK-106): presentations
- A confirmed `presentation` is written by the same `write_work` flow and job (no new Hub operation, catalog or proto:
  `homework_draft`, prompts version 2). The outline is the slide list (8–12 slides: title first, `bullets` or
  `two_columns`, conclusion last; planned words of the notes 600–1200 in all for a talk of 5–7 minutes); the student
  approves it as a paper's outline; then one slide per queue step (bullets ≤ 6, notes), then one call for 5 likely
  questions with short answers, then the files. Same key discipline: a stored slide or question set is never asked again.
- A talk already in the assignment or the material is built into the notes (prompt rule).
- Every answer is parsed strictly on the desktop (exact keys, bounds, `values.Presentation.Validate`), control and
  format (bidi, zero-width) characters are dropped; stored state is checked again on load (no migration: `works.writing`
  gains optional fields; a paper's stored form and content key are unchanged).
- Any change of the slides asks the questions again; `Regenerate(0, note)` on blocked questions asks only them again.
- Result: the `.pptx` (local renderer, template `paper` by default; no picker yet) and a speaker text `.docx` (notes by
  slide, the questions, «Сгенерировано — прочитайте перед сдачей»), both filed into the subject's «ДЗ»; Undo of
  `write_work` moves both back to «Входящие» (files kept), refused if the student moved either.

## Addendum (TASK-107): lab reports
- A confirmed `lab_report` is written by the same `write_work` flow (no Hub, catalog or proto change: `homework_draft`,
  prompts version 3, steps `lab_outline`/`lab_section`). Each outline section has a `part` (goal, equipment, theory,
  procedure, results, conclusions, other); the manual's own headings and order win, else the standard structure; at
  least one results and one conclusions section.
- The model never writes the student's data: a gap is a paragraph «▢ Вставьте …» (one clean line, ≤ 200 characters,
  ≤ 20 per section). The domain recognises and checks placeholders strictly; a results section without one is a
  refused answer (an attempt used). Conclusions are written conditionally (prompt rule).
- The app, not the model, adds to the .docx: placeholders as highlighted blocks, a note under the conclusions that
  they depend on the student's data, and the final block «Что доделать самому» (every placeholder by section, the
  conclusions, an unread manual).
- The teacher's material reaches the writer as `material_text` in the data block (cut to the writing limit) with
  `manual` = read | none | not_read (PDF, ADR-0027) | no_text (scan, empty, damaged). An unread manual is told to the
  writer, shown in the paper and in the view; the report is written from the assignment alone. No PDF reader is added.
- Storage: `works.writing` gains optional `lab` and section `part` (no migration); a paper's stored form and content
  key are unchanged; a lab report written as a paper before this stays a paper.

## Addendum (TASK-179): «Спросить по шагу»
- `StepAdvisor` (hubclient) calls `Complete` on `homework_draft` with the writing alias and a desktop prompt
  (`prompts.BuildStepAdvice`, `StepAdviceVersion` 1): no Hub, proto or catalog change, no tools.
- The step, the requirements and the student's question are one escaped JSON object in the data block, never
  instructions; a command in the question is answered as text at most.
- One paid call per question; key = `work.StepQuestion.Key` (work, requirements, step, question) + prompt version, so
  the same question again is a free replay. A model refusal or an answer failing `work.CheckAdvice` is refused;
  quota and Hub errors map as for the reader. The answer is card text only and triggers nothing; nothing is stored.

## Consequences
- Answers, edits and confirmation come only from `input/wails`; the agent can start a triage, nothing more.
- The triage outcome of a request is not fed to the plan policy yet (`Facts.Triage`); the mandatory plan covers it.
