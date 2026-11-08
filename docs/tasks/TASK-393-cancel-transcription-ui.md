# TASK-393: «Отменить расшифровку» and «Расшифровать снова» on the recording row (UI)

- **Agent:** frontend
- **Label:** frontend
- **Depends on:** TASK-392
- **PRD sections / AC:** §7.2; owner 2026-11-08; owner UI rules (small actions = icon + tooltip; main action of a
  state = word; quiet statuses; no dead ends)
- **Агент:** пока нет инструмента (row in `docs/agent-parity.md`: follow-up `cancel_transcription`, `retranscribe`)
- **Branch:** ralph/<date>-task-393

## Goal
While recognition runs, the row shows its progress and a small cancel icon; after a cancel it says so quietly and
offers «Расшифровать снова».

## Allowed to change
- `paratech/desktop/frontend/src/recordingRowView.ts`, `src/pages/RecordingRow.tsx`, `src/activityView.ts`,
  `src/todayView.ts` (only if the Today row shows the same stage), `test/*`
- `paratech/e2e/specs/recording/*` (cancel scenario: import → STT starts → cancel → «Расшифровать снова» → done)
- `docs/agent-parity.md` (one row)

## Acceptance criteria
- [ ] Stage transcribing or queued for transcription with `canCancel`: the existing progress («N %», «≈ N мин
      осталось») plus an icon button (stop/cross) with tooltip and aria-label «Отменить расшифровку»; no confirmation
      dialog (reversible by «Расшифровать снова»); disabled while the call runs; result `ending` → the row just
      refreshes; `failed` → short line «Не удалось отменить расшифровку.».
- [ ] Error code `transcriptionCancelled`: quiet status (icon + muted text, not the error colour) «Расшифровка
      отменена» and a word button «Расшифровать снова» (the main action of this state); `no_model` → «Нет модели
      распознавания» with the existing «Открыть модели» action; `started` → the row shows «В очереди».
- [ ] Russian, impersonal texts as text nodes; keyboard focus order; layout invariants of ADR-0035 §5.
- [ ] Tests: row keys for both states; one call on double click; each result code.
- [ ] `npm --prefix paratech/desktop/frontend run test` and `run build`; `npm --prefix paratech/e2e test`.
- [ ] Owner check `docs/qa/tasks/TASK-393.md`: start recognition of a long recording → cancel icon → whisper-cli gone
      in Task Manager, row «Расшифровка отменена»; restart the app → still cancelled; «Расшифровать снова» → runs to
      the summary.
</content>
</invoke>
