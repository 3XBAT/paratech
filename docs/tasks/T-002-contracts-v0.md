# T-002: Contracts v0

- **Agent:** architect
- **Label:** docs
- **Depends on:** -
- **PRD sections / AC:** §4.4, §5, §7, §9.2, §12, AC-09, AC-11, AC-21
- **Branch:** task/T-002-contracts-v0

## Goal
Define the contracts that let modules be built in parallel.

## Allowed to change
- `shared/*.proto`, `docs/adr/`, `docs/contracts/` (SQL schema draft), Go interface skeletons under `desktop/internal/**/` (interfaces only, no implementation)

## Out of scope
- Implementations

## Acceptance criteria
- [ ] Hub proto: LLM request/response with idempotency key, quota reserve/settle/release, remaining-quota query, API-key auth metadata
- [ ] SQLite schema draft: jobs (all states from PRD §7.2 incl. `WAITING_FOR_POWER`), recordings, transcripts, assignments, schedule entries, STT models, deliveries, settings
- [ ] Go interfaces: `StorageManager`, `JobStore`, `Recorder`, `STTEngine`, `PowerMonitor`, `CredentialStore`, `CalendarAdapter`, `EmailAdapter`
- [ ] Follow-up tasks T-003, T-004 written with the template
- [ ] Open questions for the user listed
