# TASK-204 — agent chats storage (ADR-0028)

## Done
- Migration `0040_agent_chats.sql` (0039 was taken by assignment_deleted): agent_threads/turns/steps/actions rebuilt with the same ids and rowids; threads lose subject_key, gain title (subject name cut to 80 chars, «Без предмета» for NONE/missing subject), archived_at, last_active_at = max(updated_at, created_at), index (archived_at, last_active_at); turns gain mentions (JSON array) and dropped_by. Down restores 0022/0023; only the newest chat per subject key survives (documented in the file). schema.sql mirrors it.
- domain/agent/thread: subject key removed (key.go deleted); `thread.New(id, now)`; Restore no longer accepts rows without title/last activity.
- cases/agentturn: `GetByKey` -> `Current(ctx)`; `IdleInput.Scope` removed; Ask goes to the current chat or creates one.
- adapters/sqlite: new fields and mentions, `Current`, `List` (unarchived by last_active_at desc, then archived), `RequestScope(thread, turn)`. The history read leaves out dropped requests in SQL (before the LIMIT). sqlc v1.31.1, no drift.
- Subject now comes from the request: input/agent/freework.go and cases/agentjournal use `RequestScope`. Stale subject-key comments cleaned.

## Existing tests changed
- thread.New calls (key argument dropped); TestKey replaced by TestNewChecksIDAndTime; chat_test legacy-restore fallback assertions became refusals.
- sqlite agent_test: GetByKey -> Current; TestAgentThreadKeyIsUnique -> TestAgentChatsAreManyAndIDsUnique.
- agentturn stuck_test: "by scope" -> "the current chat"; "another subject" -> "another chat is current" (still false); fakes GetByKey -> Current.
- agentjournal TestListMergesActionsAndProcessing: scopes per request (the "missing thread gives no subject" case is not covered any more).
- input/agent freework tests: fake serves request scopes.
- desktop/tests agent_eval TestAgentEvalProvenance: the second Ask goes to a newer chat; the check is unchanged.
- migrate_test / agent_migration_test: version 40.

## Decisions to confirm
- cases/agentjournal changed (outside the file list): forced by removing thread.Key; TASK-206 only needs to add the chat title.
- Current/List tie-break: equal last_active_at -> greater id; archive ordered by last_active_at.
- Long subject names cut to 80 chars in titles.

## Follow-ups
- A legacy thread with updated_at < created_at migrates now, but thread.Restore still rejects it (as before). Consider normalizing updated_at in a later migration or relaxing Restore.
- Proto comments "one thread per subject" (shared/proto gateway.proto:163, gateway.pb.go:543): fix with the TASK-245 contract change.
- Nothing sets dropped_by yet (TASK-248); List has no use case caller until TASK-205.
- From TASK-203 still open: Fill ignores HistoryFrom after Restart; mention provenance of earlier requests (security decision before TASK-245).
- Provenance (`seen`) is per chat now, so IDs seen under subject A are accepted in a request for subject B in the same chat; the planner history mixes subjects (ADR-0028 design, noted by reviewer/auditor).
- ADR-0028 Open 1–7 untouched; 90-day cleanup unchanged.

## Security
Auditor: no high/medium. Reviewer: approve; one medium (last_active_at CHECK vs skewed updated_at) fixed with a test.
