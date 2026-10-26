# TASK-187: turnkey work without an assignment, topic as source (core)

## Done
- Domain `coursework/work`: `Topic` value (`NewTopic`), `Source.Topic`, exactly one of assignment | upload | topic (`Source.Validate`), `Work.Sendable()` -> `ErrNoAssignment` for topic and upload works. Topic text: no control chars/newlines, format chars (bidi, zero-width) dropped, spaces collapsed, 1..500 runes; request key 1..128 visible ASCII; subject optional.
- `cases/turnkey`: `TriageTopic(ctx, request, text, subject)`; reader gets the topic as `assignment_text` (data, ADR-0022), writer too; no Hub change. `View.Sendable`. Same key = same work (no second charge); new key = new work; key reused for another topic/subject is refused.
- Storage: migration `0031_work_topics.sql` (rebuild of works/work_rounds; columns topic_request, topic, topic_subject_id; CHECKs; partial unique index). sqlc regenerated; schema.sql updated.
- ADR-0025 addendum.

## Decisions to confirm
1. Request key is supplied by the caller per explicit action (agent: thread/turn/call_id; UI: per press). TASK-188 must pass a stable key per call.
2. No FK on topic_subject_id: a removed subject sends the paper to «Входящие».
3. Format characters dropped silently; control chars/newlines refused.
4. Key reuse is blocked only while the work has not failed (ADR wording adjusted).
5. Reader gets the plain topic, with no prefix.
6. Migration 0031: if a parallel PR (TASK-185) also adds 0031, renumber at merge.

## Follow-ups
- TASK-188: `triage_assignment{topic}` -> `TriageTopic` with stable key and thread subject; verify one thread cannot replay another's key.
- TASK-189 / input/wails `TurnkeyWork` DTO: add topic text, subject, `sendable`.
- TASK-180: send rule must call `Work.Sendable()` and map `ErrNoAssignment` to `no_assignment`.

## Changed existing tests
- `adapters/sqlite/migrate_test.go`: schema version 30 -> 31.
- `cases/turnkey/fakes_test.go`: fake work store identifies a source like the real store (id, or request key for a topic).

## Security
Auditor: no findings (TriageTopic has no caller yet). Reviewer: approve; one wording fix in the ADR.
