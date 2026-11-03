# TASK-322 — wave 10 on the desktop: get_settings, set_setting, quota_status, recording{start|stop}; not served until TASK-326

## Done
- `values/wave10controls.go`: result shapes `SettingsResult`, `QuotaResult`, `RecordingResult` (started, already_recording, stopped, not_recording); `ActionLine.Offer` (an `OfferAction` that needs no ID, validated) for the «Начать запись» button after a stop.
- `input/agent` `settings.go`, `quota.go`, `recording.go`: Check / Apply / Observe / replay / Recover for the four tools through narrow interfaces (`Controls`); `app/agentcontrols.go` wires them to the page's use cases (`preferences.Service`, `connections` RefreshQuota, `recordingprompt.Service.StartManual`, `recordingsession` Stop). CatalogVersion stays 9; the four tools are invisible at 9 (tests). Headless wiring answers "unavailable".
- `adapters/sqlite/agentactions10.go` (+ one field `Prior *settingWire` in `compensationWire` of `agentactions.go`): the journal stores `RestoreSetting` and `StopRecording` (real SQLite round trip; forged rows refused by the domain).
- set_setting is an ALLOWLIST (owner: the agent never changes permission settings): the port `AgentSettings` has exactly Get + three setters (recording_prompts, ac_power_only, recognition_load); `settingOf`/`writeSetting` are explicit switches, anything else is `invalid_arguments` before any use case; get_settings iterates the local allowlist, not `values.Settings()`. free_work_notice and note_recognition are refused (tests pin the method count and that unknown names reach no use case).
- recording{start}: source = `Sources().Selected` (what the recording window preselects; whole computer when the remembered app does not run or apps cannot be listed, as the page does); the model passes no source. Undo = `StopRecording` (stops and keeps the audio) only if that recording still runs, else "already compensated". recording{stop}: DONE without a compensation, `NoUndo`, line offers `start_recording`.
- quota_status: `{"remaining_percent","resets"}` only; Hub not set up / no key / rejected key -> `unavailable`, anything else, a stale reading or limit <= 0 -> `failed`.
- Compensation kind asserted per action in tests (set_setting RestoreSetting; start StopRecording; stop none), also after Recover.
- Review fix: recording no-ops are journaled (start with a running recording: DONE without compensation; stop with none: FAILED), and `replayControl` answers from the entry without ever starting or stopping, so «Стоп» + «Продолжить» cannot stop a later lecture recording or start a new capture.
- Merged origin/master (TASK-321 notes, TASK-350): conflicts only where both sides add cases (sqlite codec switch and wire struct, ActionDeps/Actions/prepared/effect, write.go, app wiring); both kept.

## Decisions to confirm
1. set_setting version = position of the value in the setting's enum (+1); no settings-version accessor was added to `cases/preferences` (the singleton's version also moves on the remembered source). A student's change of the same setting gives «Изменено после»; set back = UNDONE without writing; ABA counts as the same state.
2. Recover: a pending recording start or stop becomes UNKNOWN (matching by time could claim the student's own recording); a pending set_setting is settled from its intent.
3. The no-op stop is a FAILED entry (the journal entry has no intent), so «Журнал» shows it as «Ошибка / Не выполнено — отменять нечего».
4. Criterion text says «четыре переключателя»; ADR-0033 §10 and TASK-342 give three (free_work_notice removed). The code follows the ADR.
5. `recordings` screen was not added to `values.Screen` (screens untouched).
6. Files beyond the task list (needed for dispatch/journaling, same pattern as TASK-320/321): `quota.go`, hooks in `actions.go`/`write.go`/`tools.go`, `values/card.go` + `wave10controls.go`, `adapters/sqlite/agentactions*.go`, `docs/agent-parity.md` notes of the four rows.

## Follow-ups
- TASK-323/324: the panel `cardView` and the frontend do not render `ActionLine.Offer` («Начать запись») yet.
- TASK-325: Hub tool texts need the result shapes and refusals (already_recording, not_recording, remaining_percent/resets, not_found when the source closed, unavailable without a Hub key).
- `agentjournal.Entry` should expose `Intent`: then the no-op start replay can name the original recording (now it names the one running at replay time or answers not_recording; nothing starts) and the no-op stop can be DONE.
- Journal view (`frontend/src/journalView.ts`): no `recording` tool label (shows «Действие»); DONE without a compensation shows the "это предложение" no-undo reason (also for a real stop) — needs its own reason text.
- `action.Complete` checks compensation kinds per tool, not per action (from TASK-319); tests assert per action meanwhile.
- Stop / Undo of a start wait for the session without a timeout (same as the page).
- Low: a recover of a pending recording start leaves UNKNOWN, no Undo.

## Changed existing tests
None changed or weakened (the two recording tests changed during the review fix are new in this task).

## Security
Auditor: no findings (allowlist at three levels, no model-chosen source, quota/get_settings leak nothing, codec fields unique after the merge). Accepted risk per О-7 а: injected text could start/stop a recording or flip a toggle; bounded by one recording at a time, no source choice, audio stays local, Undo for start and set_setting. Reviewer round 1: approve with one Medium (recording no-op replay) fixed; round 2 on the fix: approve, three Lows (journal labels, replay names the current recording, stale comments — the comments fixed).
