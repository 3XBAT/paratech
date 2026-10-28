# TASK-266 — check steps

Storage only: nothing new is visible in the app; the point is that the owner's existing database migrates safely.

1. Back up the app database file, then start the app on it (a build of this branch). Expected: it opens, the agent panel shows the same chats and requests as before; no error.
2. Ask the agent something and wait for the answer; close the app and open it again. The chat and the earlier messages are as they were.
3. From a checkout run `go -C <repo>/paratech test -race -count=1 ./desktop/internal/adapters/sqlite/... ./desktop/internal/cases/agentturn/... ./desktop/pkg/migrations/... ./desktop/tests/...`. Expected: all ok.
4. `TestMigrationAgentReplyKeepsOldRows` (adapters/sqlite/agent_reply_test.go): a version-46 database with chats migrates up, old requests read with no reply line, Down and Up again keep the rows.
5. `TestReplyLineOfAResumedRequest` (cases/agentturn/replyline_test.go): the reply line is saved when a request ends and cleared when a stopped request goes on.
