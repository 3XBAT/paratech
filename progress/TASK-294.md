# TASK-294: setting «ИИ распознаёт фото конспектов»

## Done
- Domain `settings`: `NoteRecognition` (`off` | `local`), one constant `DefaultNoteRecognition = off`, validated in the aggregate (`SetNoteRecognition`); `Restore` reads an empty or unknown stored value as `off` (fail closed). `cloud` is not added; a later engine is one constant plus a `Valid` entry.
- Migration `0045_note_recognition.sql`: `settings.note_recognition TEXT NOT NULL DEFAULT 'off' CHECK IN ('off','local')`; an existing settings row becomes `off` (migration test from a database at 44). `docs/contracts/schema.sql` mirrors it; sqlc regenerated, no drift.
- `cases/preferences`: `SetNoteRecognition` (load, change, save with version check, 3 retries like the other toggles) and `RecognitionLanguage` through the narrow port `RecognitionLanguages` (declared in the use case). No port, an error or unsupported OS -> `unknown`.
- Binding `SettingsService.SetNoteRecognition(mode)`; every Settings reply carries `noteRecognition` and `noteRecognitionLanguage` (`available` | `missing` | `unknown`). The mode is checked in the binding and again in the domain.
- Wiring: `app/ocrlanguages.go` maps `winocr.AvailableLanguages` to the port (own reader, 5 s timeout, so it never waits behind page recognition).
- Frontend: group «Конспекты» on the Settings page with the switch (off on a new install), the explanation text and, with the switch on and Russian OCR missing, the hint how to add the language. Pure helper `src/noteRecognitionView.ts`.
- The setting only stores the choice; nothing starts recognition yet (TASK-167/169). Nothing leaves the machine.

## Decisions to confirm
- Three language states instead of a boolean, so an unknown state (other OS, check failed) shows no hint.
- No version in the DTO: the use case handles the version like the other settings.
- Adding `cloud` (TASK-168) needs a rebuild of the `settings` table because SQLite cannot change a CHECK in place (the AC asks for `CHECK IN ('off','local')`). Decide before TASK-168 whether to relax the CHECK then.
- Migration number 0045 (free on master at push time).

## Follow-ups
- Reviewer Medium (deferred, outside the task files): `app/ocrlanguages.go` copies winocr's unexported `hasLanguage`. Add an exported `HasRecognizer(ctx)` to `adapters/winocr` and reduce the wrapper to a delegation.
- Reviewer Low: the UI hint says «затем вернитесь сюда», `winocr.ErrNoLanguage` says restart the app; check on a real machine whether a newly installed language is seen without restart and align the texts.
- Reviewer Low: the language check runs on every settings reply (a Warn per reply on a machine without OCR); log `ErrUnsupported` at Debug or skip while the mode is `off`. Security Low: the same check can add up to 5 s to a toggle save.
- The setting is not in the support-bundle summary.

## Changed existing tests
- `adapters/sqlite/errors_test.go`: three hand-built settings params set `NoteRecognition: "off"` (explicit params skip the column default; CHECK refuses "").
- `adapters/sqlite/migrate_test.go`: version constant 45 in `TestMigrateIsIdempotent`.
- `cases/preferences/service_test.go`: fake repo stores `NoteRecognition`; default comparisons include `off`.
- `input/wails/settings_test.go`: fake implements new methods; JSON names test has two new fields; bound-method count 7 -> 8.
- Frontend `test/agentfreeworkcard.test.mjs`, `test/loadestimate.test.mjs`, `test/settings.test.mjs`: Settings fixtures got the two new fields (parseSettings is strict).

## Security
Security-auditor: no High/Medium; one Low (above). Reviewer: approve; one Medium deferred (above).
