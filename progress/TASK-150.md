# TASK-150 — note text: Markdown, DOCX heading sections, text cache

## Done
- `material.LocatorSection` («Раздел»); `SectionsOf`; cache form `TextRef(digest)` = `texts/<sha256>`, `EncodeCache`/`DecodeCache`, `TextCacheVersion`; `shared.ArtifactTexts`.
- `material.ReadMarkdown` (pure, linear-time): front matter dropped (date/subject/tags as bounded hints, 200 runes / 20 tags), `[[x|y]]`->y, embeds and known HTML tags dropped, `#`..`###` sections when >= 2 headings, fenced code kept. Front matter only when all keys are ASCII and one is known (title/date/tags/subject/aliases/created), so plain text like `Задание: …` between `---` stays.
- doctext: TXT (no form feed) goes through `ReadMarkdown`; DOCX Heading 1-3 (built-in names via `word/styles.xml`, ids `Heading1-3` as fallback; `pPrChange` skipped) give sections; text before the first heading is its own section.
- `cases/library.MaterialText`: reads and hashes the blob, then uses the cached text or extracts and writes it via `BlobStore`; broken/oversized/mismatching cache is rebuilt; failed write only logged. No migration, no new dependency, no port change.
- Tests: table, injection fixtures, `FuzzReadMarkdown` (~60 s clean), long-line test, cache tests, DOCX section tests.

## Decisions to confirm
- Cache key is the file sha256; the text digest is stored inside and checked. The blob is still read and hashed on every call (a damaged/missing file stays `ErrFileDamaged`), so the cache saves unzip/XML only. A size-only check would be a cheap later change.
- Same «>= 2 headings» rule for DOCX; sections win over pages. Text before the first heading is a headingless section.
- Cache JSON form lives in the domain (reviewer: ARCH-31 ambiguity; alternative is a `TextCache` port + adapter).
- Hints are discarded for now; bulk import (TASK-157+) must call `ReadMarkdown` itself. Hint limits are mine (ADR-0027 Open 10 — unanswered, not decided).

## Follow-ups
- Add `shared.ArtifactTexts` to the filestore `kinds` (junction guard) and `texts`/`materials` to the whispercli work-dir check.
- «Удалить навсегда» for a material must also delete `material.TextRef(digest)`.
- `TextCacheVersion` is a manual constant: bump on any extraction change (comment added); consider a version from the extractor.
- TXT with a form feed skips `ReadMarkdown` (documented).

## Changed existing tests
- `desktop/tests/tickets_test.go` (outside the task's files): the TXT fixture `<b>?</b>` became `<w:b>?</w:b>` (and the expectations), because TXT now drops known HTML tags; the escaping check is kept.

## Security
Auditor: no findings (hardening note: `texts` kind in filestore). Reviewer: one medium (front matter eating Cyrillic-key text) fixed; two nits fixed; the rest are the follow-ups above.
