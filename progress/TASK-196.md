# TASK-196 (PAUSED)

Status: paused by the supervisor; not pushed, no PR.

## Done
- Implementation committed (a9a557ff): ADR-0020 Accepted with option (b), adapters/pdf/doc.go, html.go, html_test.go.
- Pdf package tests, architecture tests, go vet (host/windows/linux) and lint on the package pass.

## Left
- Review: reviewer and security-auditor in parallel (not started).
- Touched-scope verification of importers via heavy.sh (go vet ./desktop/..., lint).
- Bookkeeping: tasks.json status, final progress file, docs/qa/tasks/TASK-196.md.
- Fetch, bring in master, push, open the PR, run audit-pr.sh.

## Notes to confirm
- docs/architecture.md not changed (its adapter list is an example and has no docx either).
- TASK-197 must check that page numbers (@page @bottom-center) and the yellow placeholder background appear in the real PDF with ShouldPrintHeaderAndFooter=false.
- TOC has no page numbers (typography difference, recorded in the ADR).
