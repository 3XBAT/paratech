# TASK-221 — planned create_assignment with a long note

## Done
- `toolcatalog/schema.go`: `planStep` checks each step argument through `stepArgument`; for `create_assignment.note` (only if the catalog declares it as a query) the limit is `MaxQueryRunes` (500, single line, not blank), everything else stays a 80-character label.
- Test `TestPlanStepNoteLimit` (catalogv6_test.go): 81 and 500 accepted; 501, newline, blank, number refused; title/subject >80 in a create_assignment step and 81-char note of edit_due still refused.
- No catalog bump, prompt v6 and goldens untouched, desktop untouched.

## Decisions to confirm
- The provider-facing schema still advertises maxLength 80 for plan-step arguments (changing it would change the frozen catalogs/goldens; needs v7). The adapter sends no `strict` flag, so the schema is a hint and the Hub check no longer refuses a longer note. The model may still shorten planned notes; harmless since the desktop ignores `note` when matching a step (TASK-217).

## Follow-ups
- Reviewer lows: the shape comment at schema.go ~line 16 still says "string of 1-80 characters" (add the create_assignment.note exception); the hard-coded tool/arg name could become "validate each step argument by its own catalog type" (product decision, with a v7 schema).
- Reviewer note: check that desktop plan steps accept the v6 `time` argument (TASK-216/217 scope).

## Changed existing tests
None.

## Security
Auditor: no findings (single line, control characters refused, desktop re-validates, plan steps need user confirmation). Reviewer: approve.
