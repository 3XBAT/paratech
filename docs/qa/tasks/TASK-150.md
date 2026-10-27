# TASK-150 — owner's check

No UI yet (notes are wired in TASK-151+). Automated check:
1. `go test -race ./desktop/internal/domain/library/... ./desktop/internal/adapters/doctext/... ./desktop/internal/cases/library/...` from `paratech/` — passes.
2. `go test ./desktop/tests/...` — the architecture test passes.
3. Optional fuzz: `go test -run xxx -fuzz FuzzReadMarkdown -fuzztime 30s ./desktop/internal/domain/library/material/` — no failure.
Once notes are attachable: a Markdown export with `---` front matter, `[[a|b]]` links and two `#` headings shows as plain text without the front matter, with sections «Раздел»; a DOCX with Heading 1-3 gives sections too.
