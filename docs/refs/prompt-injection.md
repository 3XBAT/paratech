# Prompt injection: the side-effect boundary

Source: PRD v0.4 section 13.3, AC-14, AC-18, CLAUDE.md hard rule 3. Code: `desktop/internal/domain/coursework/proposal`,
`desktop/internal/cases/homeworkflow/proposals.go`. Introduced by TASK-040.

## The boundary

```
LLM output (values.UntrustedOutput)
  -> proposal.New        strict parse: closed kinds, closed fields per kind, bounded text      else ErrMalformed, nothing stored
  -> Proposal.Validate   check against trusted local state (proposal.Facts)                    else REJECTED with a reason
  -> Proposal.Confirm    explicit user action only                                              else never executed
  -> EmailCommand / ExportCommand / CalendarCommand   only from a CONFIRMED proposal
  -> side-effect port    HomeworkMailer / HomeworkExporter / DeadlineCalendar
```

Transcripts are sent to the LLM only as `UntrustedData`, separate from the instructions.

## Rules for new features

1. Model output stays `values.UntrustedOutput` until a domain parser turns it into a typed value. Never pass it, or a
   string read from it, to a port.
2. The model may name **references** (an assignment id) and supply **data** (titles, text, times). It never names a
   **target**: email addresses, file paths, URLs, hosts, calendars, commands. Targets come from local state
   (settings, `proposal.ExportRef`, the adapter's own calendar). Do not add a target field to the schema.
3. Parse strictly: unknown kinds, unknown fields, fields the kind does not use, trailing data, oversized or
   control-character text are rejected. Error messages name fields, never quote model text.
4. A new action is a new `proposal.Kind` with its own command type (unexported fields, built only by a method of
   a CONFIRMED proposal) and its own narrow port that takes that command. Add the port to
   `desktop/tests/sideeffect_boundary_test.go`.
5. Confirmation is a user action through the UI; nothing derived from model output may call `Confirm`.
   The user confirms what the dialog shows, including the trusted target frozen at validation.
6. Commands carry an idempotency key; adapters make a repeat with the same key harmless (hard rule 7).
7. Do not rely on the model refusing: tests use an "obedient" fake model that returns the attacker's output
   (`desktop/internal/cases/homeworkflow/injection_test.go`).
