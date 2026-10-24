# TASK-133: exam tickets core (list -> answers with citations -> coverage -> .docx in «Билеты»)

Status: done (PR open). Desktop Go only; no Hub, proto, catalog or frontend change.

## Done
- Domain `coursework/ticketset`: strict deterministic list parser (no LLM; <=100 tickets, title <=500 runes, 512 KiB, control/invisible/bidi chars stripped), status table proposed -> confirmed -> answering -> done | failed | cancelled, coverage (covered / partial / not_covered), budget estimate. At Confirm the figures the student saw are stored in the content JSON and Restore checks against them, not the live constants.
- Use case `cases/tickets`: Propose, ProposeFromMaterial, Set, List (<=50, with job id/status and computed `Stalled`), Confirm, Cancel, Unfile. Job `ticket_answers` (hidden from «Записи и задачи»): one ticket per queue step, results stored, a stored ticket is never asked again (answering's own Hub key covers a crash after a charged call). Ports declared in the use case; answering wired in `app`.
- Confirm only from `input/wails` TicketService (guard test updated); the agent has no tickets port (reflection test). Over the cap ($3, > 33 tickets) needs `acceptCost`.
- .docx via `homeworkdoc.BuildTickets` (GOST-like, source labels without recording ids, «Не найдено в ваших лекциях», coverage summary, «Сгенерировано — прочитайте перед сдачей»), filed into «Билеты» via `library.FileTickets`; Undo moves it back to «Входящие» (file kept).
- Migration 0029 `ticket_sets` (+ schema.sql), sqlite repository, binding `TicketService` (Propose, ProposeFromMaterial, Get, List, Confirm, Cancel, Undo). ADR-0025 addendum.
- Permanent Hub error fails the job instead of waiting for the network (tested at case, app and end-to-end level).

## Decisions to confirm
- Budget numbers are new: 90 000 micro-USD per ticket (typical, real worst case is several times higher) and $3 per-run cap; no runtime spend tracking.
- Ticket list parsed deterministically, not by an LLM («строгая схема» = fixed parser). `ProposeFromMaterial` does not require `has_ticket_list`; the student's pick is enough.
- A refused answer = `failed` for that ticket, run continues; quota/network wait and retry free. Coverage: not_covered shows only «Не найдено в ваших лекциях»; unsupported claims in partial are marked.
- `library.Service.Material` not-found now also wraps `shared.ErrNotFound` (outside the tickets files).
- «обратимо, план» for agent proposals is deferred with the agent tool (below).

## Follow-ups
- Agent tool `prepare_tickets` needs Hub tool catalog v3 (a Hub task); a ticket run then counts as a paid WORK job.
- Frontend TASK-134: it can list sets and see running/stalled; «Открыть в Word» is missing (no path crosses the binding).
- docs/architecture.md §12: add `ticketset` aggregate and `cases/tickets` (and `answer`).
- Purge of answers on recording delete (TASK-118 follow-up) is still open; a missing answer shows as not covered.
- sqlc was unavailable: `ticket_sets.sql.go`, `models.go`, `querier.go` are hand-written in sqlc v1.31.1 format; CI drift check decides (if it fails, regenerate).
- Lows: one corrupt row fails the whole subject list; N+1 job lookups in List; `Stalled` is true if the newest job COMPLETED while the set is still answering (cannot happen today); two sets with identical docx bytes share one material (Undo of one moves the other's file); a developer DB from an earlier state of this branch (no `accepted` key) must be reset; material text loses blank lines so a footer after the last item can join it.

## Changed existing tests
sqlite/migrate_test (version 29, ticket_sets), app/registrations_test, input/wails settings_test and shell_test (Core.Tickets), random_test, shared/agent_ids_test, library filehomework_test (+TestFileTickets) and service_test (Material not-found also ErrNotFound), tests/userconfirmation_test (tickets files allowed to call Confirm).

## Security
security-auditor: no High/Medium. Reviewer: first pass 3 mediums (pricing constants in Restore, no list/job state for the screen, budget comment) fixed; re-review approve. Hard rule 3 holds: answers are stored and rendered into a local file only.
