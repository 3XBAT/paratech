# TASK-256: CallLink value, Content.CallLink, migration, sqlite

## Done
- `values.CallLink` / `ParseCallLink` (byte-by-byte, no net/url), `String()`, `Host()`, `IsZero()` per ADR-0032. Scheme and host are lowercased into the canonical form.
- `lesson.Content.CallLink`: `Validate` re-parses and requires the canonical form; `Equal` compares it; a change goes through `ChangeContent` (revision bump).
- Migration `call_link TEXT NOT NULL DEFAULT '' CHECK (length(call_link) <= 2048)` on `schedule_entries` (the table where lessons live; the ADR says `lessons.call_link`), mirrored in `docs/contracts/schema.sql`; sqlc regenerated (v1.31.1); repository reads and writes the field.
- Google projection untouched; a test proves the link is not written.

## Decisions to confirm
- Extra rules beyond the ADR: DNS label length/hyphen rules, `xn--` refused, last host label numeric or `0x`-hex refused (browser IPv4 reading).
- ADR wording `lessons.call_link` should become `schedule_entries.call_link`.
- Stored links are re-parsed on load, so one failing row fails the schedule read. Chosen: tightening `ParseCallLink` later needs a clearing migration (noted in `values/doc.go`). Alternative: `Restore` treating a bad link as none.

## Follow-ups
- TASK-257: `importplan.writeContent` (plan token) must hash `CallLink` (`field(c.CallLink)`) with a test before ICS extraction lands.
- ADR-0032 Open 1-4 untouched.

## Changed existing tests
- `adapters/sqlite/lessons_test.go` `TestMigrationToVersion14AddsLessonDetails`: migrates to the latest version before `Get` (generated SELECT now names `call_link`); assertions unchanged.
- `adapters/sqlite/migrate_test.go`: latest-version constant.
- New test-only file outside the task list: `adapters/googlecal/calllink_test.go`.

## Security
Reviewer: approve. Security-auditor: no findings.
