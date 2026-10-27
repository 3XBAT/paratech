# TASK-203 — chat thread domain (ADR-0028)

## Done
Domain only, `paratech/desktop/internal/domain/agent/{thread,values}`:
- thread: title (default «Новый чат», auto from the first student text, 40 runes at a word boundary), archivedAt, lastActive; Rename (1–80 runes, no control/bidi), Archive (only without a running request), Restore; a new request un-archives; IsEmpty.
- values.Mention (subject|lesson|material|assignment, display-only Label ≤200 runes, ≤8 per request); mention IDs join the provenance check for the current request; Label never reaches HistoryTurn/Hub (reflection test).
- Window: 20 pairs / 48 KiB / 64 turns, start moves in blocks of 5 requests (16–20 earlier answers, byte-identical prefix; 30-request test). `DroppedBy` on history entries (set only in TASK-248), skipped by Window and Fill. `thread.Fill` -> Pairs/Bytes/Turns/Share/Overflow, tested against Window.
- The "scope subject = thread key" rule is removed (Key/KeyOf/SubjectKey/ParseKey stay until TASK-204). Restore accepts old rows without title/lastActive (fallback «Новый чат»; TASK-204 can drop it).
- No callers outside the package changed.

## Existing tests changed
- thread_test.go TestWindowStart: rewritten for unexported `windowStart` (exported WindowStart removed, no outside callers); block-step cases added.
- calls_test.go TestWindow: 20 requests now send all 19 earlier pairs (was 12 from request 8).
- reply_test.go TestWindowCutsWholePairs: 12->20 requests, 24->48 KiB, the 64-turn case sends 7 pairs (start moves a whole block).
- calls_test.go TestStartRequestScope: one chat now accepts requests of two subjects; Restore accepts any scope.

## Decisions to confirm
- Only the current request's mention IDs count for provenance. ADR-0028 sends earlier requests' mentions to the Hub too; if the model may cite them, they need a thread-level rule (TASK-245).
- Rename to exactly «Новый чат» is overwritten by the next auto-title (no "named" flag).
- Title chars: control + bidi only (as the task says); mention labels use unicode.IsGraphic (stricter). Could unify.

## Follow-ups
- Dropped requests burn the history read limit (agentturn service.go:42, 357 entries) and break 5-block alignment when a read is cut mid-block: exclude dropped rows in SQL / count live requests (TASK-204/248).
- Fill ignores HistoryFrom after Restart (Overflow false though the agent forgot older requests): handle when the ring is wired.
- Stale comment input/agent/turnkey.go:65; agentjournal/service.go:387 and input/agent/freework.go:110 still use th.Key().Subject() (valid until TASK-204).
- Window is live in agentturn now: per-step history can double (12->20 pairs, 24->48 KiB) before the cache layout of catalog 8 pays off.
- Fill returns Share as a fraction only; no percent/level.

## Questions on ADR-0028 Open items (not built)
Open 1 (ring content; with variant (а) a chat past 21 requests stays at 0.8–1.0 with Overflow), Open 2 (deleted/renamed mention objects: label kept as at mention time), Open 3 (dropped-request UI), Open 4/6 (no summary, no cleanup change). Opens 5, 7: nothing built.

## Security
Auditor: no high/medium. Low: title check lacks IsGraphic (invisible format chars allowed); titles go to no side effect.
