# TASK-253 — part counts in «Мой конспект»

## Done
- Domain: `material.TextCounts{Sections, Pages, Fragments}` and `Text.Counts()`.
- `cases/notes`: `Materials` port gets `MaterialText` (`library.Service` already has it, no new adapter); `View.Counts` aligned with `Parts`; `counts.go` has an in-memory cache by file sha256 (4096 entries, cleared when full) and a budget of 200 text reads per listing.
- Binding `NotePart`: `sections`, `pages`, `fragments` (active parts only; 0 for removed/missing/damaged/no text).
- Frontend: `bindings/notes.ts` normalises counts; `partPiecesText` shows «N разделов» else «N стр.» else «N фрагментов» (Russian plurals); rendered as a text node.

## Decisions to confirm
- `fragments` = all text pieces; `sections`/`pages` count only their kind, so at most one of them is non-zero. UI precedence: sections, pages, fragments.
- Photo parts get pages 0 (already counted by `noTextPages`). DOCX page breaks / TXT form feeds already give pages before TASK-156; PDF pages appear on their own after it.
- PPTX slides read «N фрагментов»; plain TXT reads «1 фрагмент» (product wording: «слайдов»?).
- Errors (missing, removed, damaged, store failure) are not cached and never fail the list; unreadable/no-text results are cached as zero.

## Follow-ups
- Reviewer medium: a cache miss calls `MaterialText`, which reads and hashes the whole blob (up to 100 MiB) synchronously in list/view (up to 200 parts), and no singleflight. Better: store counts next to the text cache or the text-status row so listings never touch blobs.
- Reviewer low: one Warn per failed part per listing; log once per listing with a count.

## Changed existing tests
`cases/notes/fakes_test.go`: fake library gains `MaterialText` (port grew), keeps bytes/texts/errors per material and a read counter; `filed` calls the new `filedAs`, same behaviour.

## Verification
go test -race of cases/notes, domain/library, cases/library, input/wails, app, desktop/tests (architecture); go vet (+GOOS=windows); lint v2.14.0 clean; npm test 895 pass, build OK.

## Security
Auditor: no findings. Reviewer: approve (1 medium, 1 low, 2 nits above).
