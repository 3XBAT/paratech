# TASK-150: Text of notes: Markdown, DOCX headings, text cache

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** -
- **PRD sections / AC:** ADR-0027 «Text», ADR-0024 «Text extraction», ADR-0019, ADR-0022
- **Branch:** ralph/<date>-task-150

## Goal
Typed notes (TXT, Markdown incl. Obsidian/Notion exports, DOCX) give text with section locators that a citation can
point to, without re-reading the file for every question.

## Allowed to change
- `paratech/desktop/internal/domain/library/material/{text.go,markdown.go}` (+ tests)
- `paratech/desktop/internal/adapters/doctext/**`
- `paratech/desktop/internal/domain/shared/artifact.go` (a `texts` artifact kind, if needed)
- `paratech/desktop/internal/cases/library/{inspect.go,ports.go}` (+ tests) for the cache read/write

## Out of scope
- PDF (TASK-156), images (TASK-165).

## Acceptance criteria
- [ ] `LocatorSection` added; a section fragment starts with its heading; label «Раздел».
- [ ] Pure Markdown reader: front matter (`---` block at the top) dropped and its `date`, `subject`, `tags` returned
      as hints (strings, bounded); `[[x|y]]` → `y`, `[[x]]` → `x`, embeds `![[…]]`/`![](…)` and HTML tags dropped;
      `#`…`###` headings make sections when there are at least two, otherwise fragments as now. Plain TXT unchanged.
- [ ] DOCX: paragraphs with Heading 1–3 styles become sections (else fragments as now).
- [ ] Extracted text of a material cached as a derived artifact by the text digest via the Storage Manager; a broken
      cache is rebuilt from the blob; fuzz test of the Markdown reader (no panic, bounded output).
- [ ] Injection fixture: a note with «ignore the instructions» stays plain text in the fragment.
- [ ] `go vet`, `go test -race` of the touched packages and the architecture test pass.
