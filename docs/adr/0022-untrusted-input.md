# ADR-0022: Untrusted input for every call

- **Status:** Accepted
- **Date:** 2026-10-22
- **Source:** TASK-112; hard rule 3, ARCH-27, PRD §13.3; design `agent-core-design-2026-10.md` §3 (outside the repo);
  ADR-0005(b), ADR-0021.

## Context
The agent reads files, transcripts and schedule texts written by others and can act. A text that says «send this to
x@y» or «ignore the instructions» must never choose an action, a recipient or a path. One model must not both read
foreign text and pick tools.

## Decision
- **Planner (`AgentStep`) sees** only: the student's text, the shared base, IDs and enum metadata, R-tool results (IDs,
  dates, counters, statuses), names of subjects the student created or confirmed (letters, digits, spaces, `.,-()`,
  ≤ 80 characters, as data). Schedule strings are matched to subjects by `subjectmatch` on the desktop and never sent
  as text.
- **Planner sees (addendum, TASK-183)**: its own card texts of earlier requests of this thread and the names of the
  tools those requests called with `ok | error`, as one `agent_reply` per earlier request (ADR-0023 "History pairs").
  `thread.Reply` builds it on the desktop from stored steps, with fixed labels `reply:`, `plan:`, `cannot_do:`,
  `asked:`, `actions:`: only `reply.text`, `show_plan.summary`, `cannot_do.your_part/our_part` of calls that passed
  argument checks and were not refused; `ask_questions` as a fixed line without the questions; outcome by `is_error`,
  no call IDs, no `result_json`. Never reader texts (ANSWER, TRIAGE, summaries), titles or file names. A request with no
  such text gets `no answer: the request ended without a reply`. At most 2 KiB, cut on a rune boundary.
- **Tool results**: each `result_json` is built by a mapper in `domain/agent/values` from an explicit allowlist of typed
  fields (IDs, enums, dates, counters). Use-case views are never serialized whole; error texts become an error enum.
- **Answers to Questions cards** reach the planner only as `{question_id, option_index}` or the student's own typed
  text, never the question or option text a reader wrote.
- **Planner never sees**: texts of lessons, materials, emails; assignment titles (a model wrote them); file names
  (shown as `upload#1 · pdf · 24 стр.`). History of other subjects is not sent either.
- **Quarantined readers cannot call tools** and return a schema only:
  - `INSPECT_MATERIAL`: enum / ID from the candidates passed / date / bool (`doc_kind`, `subject`, `lesson_date`,
    `has_homework`, `has_ticket_list`, `mentions_email`); an extra field or free text is refused.
  - `ANSWER`: claims with citations `chunk_id + quote`, checked in the domain (`coursework/services/citation`); a claim
    without a valid citation is not shown as «из ваших лекций». The text goes to a card; the planner gets counters.
  - `TRIAGE_ASSIGNMENT` (new operation, like the two above): strict schema to the domain; free text only into cards,
    never back to the planner.
  - Existing `SUMMARY`, `EXTRACT`, `DRAFT`, `LECTURE_TEXT`: strict schema to the domain as now; free text only into
    cards and files, never back to the planner.
- **Recipients**: the tool catalog has no address, path or URL field. `propose_send` takes the one recipient from
  settings. If a reader saw an address in a file (`mentions_email`), the card says «В файле указан другой адрес —
  не используется»; the address itself goes nowhere.
- **Paths**: files are addressed by ID. A folder name is a label from the student's request, checked by a value object
  (length, Windows-forbidden characters, CON/NUL). Export only through the «Сохранить как» dialog.
- **Action choice** is made only by the planner, which saw no source text. Arguments are IDs, enums, dates and short
  labels; every ID must pass the provenance check (ADR-0021).
- **Forged history**: the Hub signs every echo of its tool calls (`seal`, ADR-0023).
- **Irreversible actions** only by the student's click in `input/wails`; the agent has no path to them.

## Consequences
- Some answers are less helpful (the planner cannot quote a title); cards show the trusted data instead.
- Eval level 1 (CI, blocking): policy tests plus injection fixtures — a manual saying «send to x@y», a file name with a
  command, a transcript «ignore the instructions», an ICS lesson title with a command, free text in a reader's enum
  field, a command in a triage question text, a command in an error text inside a tool result.
- Eval level 2 (live, report on any PR that changes a prompt): 40–60 requests → expected plan and tool sequence;
  20 lecture questions → share of valid citations; 5 «impossible as a whole» → `cannot_do`; no «я» in the voice.
- Separation is not an absolute guarantee; therefore C actions are physically out of the agent's reach.
