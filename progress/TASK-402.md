# TASK-402 — файл билетов делится на части, когда не помещается в предел документа

Owner answer 6 of 2026-11-08; follow-up of `progress/TASK-366.md`. Go layer by go-core, TS binding by frontend (parallel, contract fixed first).

## Done
- `homeworkdoc.BuildTicketParts`: one document when it passes `Validate`, else consecutive parts (a ticket is never split). The size of a part is found by binary search over the ticket count (a longer part never fits when a shorter one does not). Heading `… (билеты 1–50)`, file `Билеты <предмет> 1–50.docx`; a one-ticket part says `(билет N)`. Each part has its own coverage summary, table of contents and mark. A ticket that does not fit alone is cut at the last whole claim (binary search over its answer blocks) and ends with «Ответ сокращён: не поместился в файл.»; it is always its own part. `BuildTickets` is unchanged in output.
- File names keep the range even for a long subject name and stay within 255 bytes (`MaxFileNameBytes`).
- `ticketset`: `File{Material, Artifact, From, To, Name}`; `Content.Files` (`files`); `Finish([]File)`, `Files()` instead of `File()`. `ParseContent` still reads the old `file` key (one file, no range, no migration); both keys is `ErrInvalidSet`. Validation in the aggregate: done exactly when it has files, at most `MaxTickets`, distinct materials, ranges consecutive over the tickets exactly (a single file may have no range — sets filed before this task).
- `cases/tickets`: `assemble` builds, validates and renders every part before filing any; a repeated step finds the same materials (same bytes); a cancel meanwhile moves every part just filed back to «Входящие» (`inTickets` guard). `Unfile` handles all parts. `View.Files` (`FileView`), `View.Material`/`Result` = first part; a legacy file gets the whole range in the view.
- `input/wails`: `TicketSet.Files []TicketFile{materialId, from, to, name}` (never null); `materialId`/`result` = first part (the DTO never had a `file` field).
- `frontend/src/bindings/tickets.ts`: `files` parsed strictly; absent/null -> derived from `materialId` (one part with from=to=0, name ""), so an older reply still parses. UI unchanged (TASK-403).

## Decisions to confirm
- Undo of a split set that the student partly moved: the parts still in «Билеты» go back, the rest stay; `ErrChanged` only when nothing moved. No «left alone» count in the view yet (TASK-403 may want it).
- A set that fits one document now stores From/To = first/last ticket and Name; only sets filed before this task have 0.

## Follow-ups
- Orphaned parts (reviewer medium, not fixed): if filing stops between two parts for good (attempts used up, or the set is cancelled before the retry), the parts already filed stay in «Билеты» outside any set. Same class as the single-file gap before, now wider. Fix: on a cancelled set in `Handle`, rebuild the parts and unfile those found by digest, or record the filed ids before `Finish`. The file is an app-made copy; regeneration finds the same bytes, no data is lost.
- TASK-403: `TicketsPage.tsx` and `ticketsView.ts` still use only `materialId`, so a split set shows only part 1 until it lands.
- ADR addendum (not edited): ticket set content holds `files` (from/to/name), legacy `file` is read; a ticket too large for a file is cut at the last whole claim.
- Optional: require a non-empty `Name` when ranges are present (harmless now).

## Changed existing tests
`ticketset_test.go` `TestRunStepByStep` (`Finish(File{..})` -> `Finish([]File{{..}})`, plus `Finish(nil)` refused) and `TestContentRoundTripAndRestore` (`Content.File` -> `Content.Files`): API change. `cases/tickets/fakes_test.go`: `fakeShelf` got an `onFile` hook and a call counter, behaviour unchanged.

## Verification
`go vet` and `go test -race` over `domain/coursework/...`, `cases/tickets/...`, `input/wails/...`, `adapters/sqlite/...`, `app/...`; `desktop/tests` arch tests and `TestTicketsEndToEnd`; `GOOS=windows go vet ./desktop/...`; golangci-lint v2.14.0 on the touched packages (0 issues); `npm run test` (2377 tests) and `run build`.

## Review
Reviewer: approve; one medium (orphaned parts, follow-up above), lows (Undo partial, Encode comment — fixed, empty Name, comment on cancel loop). Security-auditor: no findings.
