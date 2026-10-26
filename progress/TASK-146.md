# TASK-146 — desktop reads ANSWER as a stream

## Done
- `answer.Draft(partial) []string` (domain/coursework/answer/draft.go): tolerant scanner over the root `claims` array; decodes escapes, drops control chars (keeps `\n`, `\t`, like `boundedText`), max 8 texts x 400 runes, depth cap 32; fuzz + prefix-property tests.
- `answering.Reader.AnswerQuestion(ctx, req, partial)`; hubclient uses `CompleteStream` (same spec and key), falls back to `Complete` on Unimplemented, stream without result or data after result = permanent ErrBadResponse (text discarded), deltas capped at maxResponseBytes. Returned output is always the result message, never the joined deltas.
- `answering.Ask(..., drafts DraftSink)`: sink called only on change with a copy; the draft is never stored or returned; parse and citation check only on the full output.

## Decisions to confirm
- `Draft` keeps `\n` and `\t` ("control characters removed" read as in final claim text). Cf characters (bidi) pass, same as `boundedText`.

## Changed existing tests / out-of-list files (signature-only edits)
- input/agent/answer.go (+test), app/tickets.go (+test), tests/tickets_test.go, tests/answering_sqlite_test.go: new `Ask` argument (`nil`). hubclient answer_test and answering service_test updated for the new signature; the old hubclient tests now exercise the Complete fallback (FakeHub has no CompleteStream).

## Follow-ups
- TASK-147: bind a real sink in input/agent; throttle (Draft rescans the whole accumulated text per delta, O(n^2) worst case); render as plain text marked «черновик»; consider stripping bidi (Cf) in the UI.
- Test gap (reviewer): call timeout firing mid-stream is not covered (shared path with unary).

## Security
Auditor: no high/medium. Lows: bidi/Cf passes the draft, quadratic rescan; both for TASK-147. Reviewer: approve.
