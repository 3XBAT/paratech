# TASK-168: Hub operation for reading handwritten pages (BLOCKED)

- **Agent:** hub-dev (opus)
- **Label:** hub
- **Depends on:** **owner decision ADR-0027 Open 3 = option D**
- **PRD sections / AC:** ADR-0027 «Handwriting and scans»; PRD §9.2, §13.2; AC-11, AC-25; ADR-0010, ADR-0023
- **Branch:** ralph/<date>-task-168

## Goal
The Hub reads a page image with a vision model and returns its text in a strict schema, quota-controlled.

## Blocked
Only if the owner chooses to send page images to the cloud (option D).

## Allowed to change
- `paratech/shared/proto/**` (an image part on `Complete` for one new operation type; `buf breaking` green)
- `paratech/hub/internal/cases/gateway/**`, `paratech/hub/internal/adapters/anthropic/**`
- `paratech/hub/pkg/prompts/ops/read_page/v1/**`, `paratech/hub/pkg/migrations/<next>_*.sql` (operation label)
- `docs/refs/anthropic.md` (image input facts from the official docs)

## Acceptance criteria
- [ ] One image per call, JPEG/PNG, ≤ 5 MiB and the provider's pixel limit, checked before reserving quota.
- [ ] Strict output schema (lines with an uncertainty flag); free text refused; the page text is data, no tools.
- [ ] Own operation label in the quota ledger; idempotent by key; images not logged or stored after the call.
