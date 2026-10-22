# TASK-127: agent irreversible actions (propose_deadline, propose_send, propose_delete_recording), confirm card

Done: the three C tools only create a proposal / deletion preview, write a PROPOSED journal line and publish a confirm card; input/agent has no path to execution (parser test TestAgentHasNoPathToIrreversibleActions + control). New ProposeDeadline (assignment id -> create_deadline_event, reuses a waiting one), ProposeEmailOfVersion (version must equal the student's text export; recipient from settings; file pinned by digest; reuses a waiting identical send), idempotent ConfirmDeadline, library.MentionsEmail (flag only, address never stored). Wails binding input/wails/agentconfirm.go: Card, Confirm, Decline, ProposeDeadline (for TASK-128 and Today). Card shows «В файле указан другой адрес — не используется». End-to-end SQLite test with injection fixture (extra "to" arg refused, double click = one delivery, UNKNOWN delivery -> no resend). C tool in pending_driver fixtures now covered. No migration, no new dependency.

Review: reviewer M1 (replayed propose_send could create a second send proposal) fixed with a regression test; lows: log ctx fixed, deadline dedup comment softened. Re-review not repeated (small fix). Security auditor: no findings.

## Decisions to confirm
- Version ids `v<n>`; only the latest shown in list_assignments.
- Agent never exports: propose_send works only if the text export already holds that version; Word not supported for agent sends.
- Deletion is confirmed in the existing RecordingDeleteService window; «Нет» on a preview changes only the UI (journal stays PROPOSED).
- «Другой адрес» notice comes only from the assignment's source material.
- Calendar not connected: proposal stays CONFIRMED, next «Да» retries.

## Follow-ups
- Hub planner prompt does not describe the new `version` field of list_assignments.
- Deadline/send dedup is check-then-insert (harmless, calendar upserts).
- Other pending_driver fixtures (ICS title through loop, result-mapper allowlist): TASK-117/121.
- TASK-128: UI must render titles as text; no TS wrapper yet.

## Changed existing tests
tests/userconfirmation_test.go (ConfirmDeadline click-only, agentconfirm.go allowed), tests/agent_eval_test.go (comment), wails/shell_test.go and settings_test.go (new Core.AgentConfirm).

Security: hard rule 3 held; recipient only from settings; auditor no findings.
