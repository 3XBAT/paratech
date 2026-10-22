# TASK-122: material text, inspect, summary and homework search by material (Hub + desktop core)

Done
- Hub: INSPECT_MATERIAL reader, no proto change (existing Complete RPC). Server prompt `pkg/prompts/ops/inspect_material/v1` (folder named after the operation, not `inspect`), domain service `services/inspect` (strict input and answer parser: exact keys, no duplicates, enum, subject only from candidates, date), gateway: bad answer is charged, stored as `{}` and replayed free, canonical output only. Wire shape in paratech/README.md.
- Desktop: `adapters/doctext` (DOCX, PPTX, TXT UTF-8/16, PDF = not extracted; zip caps), `library.Inspect` (re-hash of the blob, strict parse, cache per text digest, 3 attempts), migration 0025 `material_inspections` + `material_id` on summaries/assignments, job types `material_summary` / `material_homework`, agent tools inspect_upload / make_summary(material) / find_homework(material). Fixture reader-enum-free-text enforced on both sides.
- Migration was 0024, renumbered to 0025 after TASK-125 took 0024.

Review: reviewer approve; M1 (Inspect bumped materials.version and broke Undo of place/link) fixed: text status is saved by a separate CAS query without a version bump, regression tests on real SQLite; L1 Hub startup warning, L2 comments fixed; re-review approve. Security auditor: no High/Medium.

## Decisions to confirm
- Prompt folder `ops/inspect_material/v1` (task text said `ops/inspect/v1`).
- Material jobs are hidden from «Записи и задачи»; shown only in the agent panel/journal.
- A material job of the same kind cannot start while one is pending/running.
- `updated_at` of a material no longer moves when its text is read.
- Hub with a reader alias missing from the allowlist now starts with the operation off and a warning (was: start failed).

## Follow-ups
- Summary/extraction prompts still call the text a transcript; page/slide locators reach the model only as headings (no citation field).
- «В файле указан другой адрес» card for mentions_email not built; no UI read of a material summary.
- gateway.proto / errors.proto comment says PROVIDER_ERROR = released; untrue for inspect schema refusal (and AgentStep). Comment-only contract change.
- Idempotency key of the reader includes `today`: a lost reply retried on a later day is charged twice (low).
- Small rules in use cases (MaterialJobs.Start active/PDF guard, removed-subject drop): could move to the aggregate.
- Hard-deleted material leaves its summary row (soft delete only today).
- Save returns a Material whose text status may be stale after a concurrent SaveText (re-Get when needed).
- Unrelated flake seen once under load: audiocapture TestRecorderCheckLevels/fail (passes alone).

## Changed existing tests
migrate_test (v25, new table), tests/schema_test, cases/library fakes_test (blob Open, Save keeps text status, SaveText), homeworkflow postprocess_fakes_test, app/registrations_test (two job types, hidden), tests/ports_test (PRD refs), hub pkg/prompts prompts_test (inspect_material no longer "missing"), domain material text_test (SetText signature), library inspect_test (version unchanged). All additive or signature-driven.
