# TASK-265: history summary, Compact, CompactionPlan, frozen reply line (domain)

## Done
- values: `HistorySummary` / `ParseHistorySummary` (1-4000 runes, <= 8 KiB, printable + newline, no control/bidi), `ErrInvalidSummary`, `HistoryTurn.Summary` turn kind, `HistoryEntry.ReplyLine`.
- thread: `Summary()/SummaryAt()`, `Compact(summary, now)`, `clearSummaryFrom(n)` (for RestartFrom, TASK-248), `FreezeReply(entries, now)`, Restore checks for summary and reply line.
- `Window/Fill(entries, current, summary)`, new `CompactionPlan(entries, current, summary) (from, through, ok)`, `CompactPairs = 10`. Blocks of 5 counted from `Through+1`.
- Summary text never enters `seen`; it appears only in the summary turn (test).
- Tests: parse table, Compact table, Restore, Fill/Window/Plan agreement (14 cases), 40-request prefix append-only test.
- One line outside the files: `cases/agentturn/step.go` passes `c.th.Summary()` to Window (always empty until storage).

## Changed existing tests
`window_test.go`, `reply_test.go`, `calls_test.go`: extra `noSummary` argument to Window/Fill; `thread_test.go` TestWindowStart: `windowStart(pairs, 0, nowTurns, 0)`. No expectations changed.

## Decisions to confirm
1. A STOPPED request that resumes loses its reply line (re-frozen at the real end), and a summary covering it is dropped. So TASK-266 must save the current request's reply_line on every Save (including clearing), not "once".
2. Compact may cover the current request when it is final (compaction runs before Ask); ADR wording "below the current request" means the next one.
3. Blocks stay aligned on request numbers counted from Through+1.
4. Plan takes the oldest 10 live pairs; dropped requests in between are covered by number only.
5. Overflow from a request cut by the read limit also triggers a plan (consistent with Fill); that request's content does not enter the summary.

## Follow-ups
- `cases/agentchats/earlier.go` does not use the frozen reply line: it is in planner format (`reply:`/`plan:`), not display captions; decide a display line or a mapping before TASK-266/271.
- hubclient `turnOf` (adapters/hubclient/agent.go) has no Summary case (would send an empty results turn); TASK-268 must add it (fail closed on unknown kinds) before anything calls Compact.
- TASK-268: domain helper exposing the plan's pairs for the summarizer; Compact is not restricted to Ask time (policy).
- Restore does not check SummaryAt <= UpdatedAt (cosmetic).
- Open 8-9 of ADR 0028 untouched.

## Security
Auditor: no findings. Summary is data only, validated on parse and Restore, never in provenance.
