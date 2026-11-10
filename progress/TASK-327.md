# TASK-327 — wave 11 tool specs and policy rules on the desktop (not served)

## Done
`CatalogVersion` stays 10; catalog 10 sees nothing of wave 11. `values.CatalogWave11 = 11`: `ToolsAt(11)` = v10 with `reply` and `set_setting` of v11, then the nine tools. `set_call_link` / `clear_call_link` skipped on purpose (ADR-0038: `set_call_links`, TASK-434/436).

Declared (the Hub, TASK-442, mirrors them):
| Tool | Class | Arguments | Card / Undo |
|---|---|---|---|
| `set_teacher` | C | `subject` id, `role` lecture/practice/lab, `name` label ≤ 80, `email` | card `teacher` |
| `clear_teacher` | C | `subject`, `role` | card `teacher` |
| `set_profile_field` | C | `field` full_name/group/university/faculty/city, `value` profile_value | card `profile_field` |
| `move_to_inbox` | U | `material` | `place_back` |
| `unlink_material` | U | `material`, `lesson` | `relink_material` |
| `remove_material` | U | `material` | `restore_material` |
| `add_alias` | U | `subject`, `suggestion` (new ID kind) | `remove_alias` |
| `remove_folder` | U | `subject`, `folder` | `recreate_folder` |
| `remove_subject` | U | `subject` | `restore_subject` |

- `reply` v11: optional `chat_title` (type `chat_title`, ≤ 60 chars, collapsed spaces, no control/bidi/format chars, no `<` `>`); an invalid title is dropped, the reply stays (`ParseChatTitle` is exported for TASK-330).
- `set_setting` v11: settings `calendar_update_on_due_change` (ask|update|keep) and `notes_ocr` (off|local; `cloud` refused). `SettingsAt(v)`; `Settings()` lists all five so stored actions restore.
- Value types (format and length only; the page's rules run in TASK-438/436): `email` (≤ 254 bytes), `call_link` (≤ 2048 bytes, no tool uses it yet; for TASK-434 rows), `profile_value` (≤ 200 chars), `chat_title`. Email, call link and profile value print as `[redacted]` in fmt/slog; parse errors never echo a value. `Args` and `Call` now print argument names only (all tools; before, `%v` printed the values).
- actionpolicy: rules for all nine; `Facts.SubjectEmpty` — `remove_subject` needs a plan (`ReasonRemovesContent`) unless the case proved the subject empty (fail-closed; zero value = plan).
- action: compensations `PlaceBack`, `RelinkMaterial`, `RemoveAlias`, `RecreateFolder` (prior state pinned to the journaled Intent.Prior), `RestoreMaterial`, `RestoreSubject`.

## Decisions to confirm
- `set_teacher` needs both name and e-mail (as `subject.NewTeacher`); name is a label ≤ 80 (page allows 200; a longer name needs its own type).
- `unlink_material` takes `lesson`, `remove_folder` takes `subject` (the use cases need them).
- `suggestion` is a new ID kind: `SuggestSubjects` returns names, so TASK-329 derives an ID per suggestion (as schedule groups do).
- `set_teacher` and `set_profile_field` cannot be plan steps (their value types are not plan-step types); they run as their own card (ADR-0033 О-10, plan runner TASK-335).
- Clearing a profile field through the agent is not covered (`value` non-empty).
- `remove_subject` Undo is `RestoreSubject`, which works only for a soft removal (`DiscardSubject`): TASK-329 must use that path for every `remove_subject`; parity row «Удалить пустой предмет → `RemoveSubject`» (hard delete) needs an update.

## Follow-ups
- TASK-329: `thread.Facts` has no `SubjectEmpty` (Admit never forwards it, so every `remove_subject` needs a plan until it does); the sqlite adapter (`agentactions10.go`) has no encode/decode for the six new compensation kinds; dispatch, settings allowlist.
- TASK-438: cards `teacher` / `profile_field`, `ArgCallLink` consumer is TASK-434/436.
- TASK-442: mirror types and limits; `chat_title` accept-and-ignore (or refuse) decision; email/profile_value never plan-step types.
- Reviewer lows left: dead per-field profile limit map (all five fields 200); style nits (`redactedValue` in the Limits const block, one-entry const block in compensation11.go); `TestStoredDataOfCatalog10UnderCatalog11` compares argument names only.

## Changed existing tests
`values/wave10_test.go` `TestCatalog10` (later catalog = newest declared, now 11) and `TestSettingValues` (`SettingsAt(10)`); `actionpolicy/policy_test.go` `TestEveryToolHasARule` iterates catalog 11 (broader). None weakened.

## Review and security
Reviewer (opus): approve, two mediums (fail-open `RemovesContent` default; empty-subject Undo assumption) fixed / recorded above, lows above. Security-auditor (opus): one medium (same fail-open fact, not forwarded by `thread.Admit`) fixed by inverting to fail-closed `SubjectEmpty`; low (no `canon` for the new value types) fixed. Parsers, redaction, class table, catalog-10 exposure, compensation trust: clean. Re-review not run (small fixes, tests added).
