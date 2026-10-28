# TASK-295 — section heading in search hits

## Done
- `kb.Hit.Heading`: `Search.Scan` fills it for own-note hits with a `section` locator (`fragmentHeading`), using the same `headingOf` as `Passages` (first line, plain, one line). New `heading.go`: `MaxHeadingRunes = 120` (pinned equal to `answer.MaxHeadingRunes` by a test) and `cutHeading` (119 runes + «…», same cut as `cases/answering`).
- `Passage.Heading` now arrives already cut to 120 runes (answering's own cut is a no-op; `cases/answering` untouched).
- Binding `ownNoteLabel` passes `h.Heading` for section locators; `answer.Source.OwnNoteLabel()` stays the only label function, so search and answers say the same: «ваш конспект · раздел „Ряды“», no heading «… раздел N».
- Tests: domain `heading_test.go` (match in heading/body, long, bidi/zero-width/control/CR/LF/U+2028, markup stays text, other locators and sections have no heading); binding `TestKnowledgeOwnNoteHits` rows (heading, none, command-like, markup, page/slide/fragment).

## Decisions to confirm
- Text before the first heading of a document is a headingless section (TASK-150); its first line is shown as the heading («раздел „Ряд Тейлора сходится при…“»), as `Passages` and answer citations already did. Fix needs a "has heading" flag from `material`/cases mapping (outside this task).

## Follow-ups
- Carry "section has a heading" from library to coursework (`NoteFragment`), so the preamble reads «раздел 1» (reviewer medium, also lows: `headingOf` on a first line of only invisible chars, a `"\xff"` case in `TestHeadingOf`).
- Drop the now redundant cut in `cases/answering/service.go` `headingOf`; optionally use `answer.MaxHeadingRunes` directly.
- TASK-287 (ranking) moves the own-note block of `Scan`: keep the line `s.hits[i].Heading = fragmentHeading(f)` in that loop.

## Changed existing tests
`wails` `TestKnowledgeOwnNoteHits`: rows got a name and a heading field, new rows added; the four original rows and expected labels are unchanged.

## Security
Auditor: 0 found (heading is plain text, bidi/format/control stripped, one line, 120 runes; the label is a React text node; the heading does not reach an LLM prompt or a side effect). Reviewer: approve, no blockers; mediums above are follow-ups.
