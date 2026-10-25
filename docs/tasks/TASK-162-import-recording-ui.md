# TASK-162: «Импортировать запись лекции» (UI)

- **Agent:** frontend
- **Label:** frontend
- **Depends on:** TASK-161
- **PRD sections / AC:** ADR-0027 «Old recordings»; ADR-0016 (estimates); PRD §7.4
- **Branch:** ralph/<date>-task-162

## Goal
The student picks an old audio file, says which lecture it is, and follows its transcription like any recording.

## Allowed to change
- `paratech/desktop/frontend/src/pages/{RecordingsPage.tsx,RecordingRow.tsx,KnowledgeBasePage.tsx,RecordingImport*.tsx}`
- `paratech/desktop/frontend/src/{recordingImportView.ts,recordingImportView.test.ts,recordingRowView.ts}`

## Out of scope
- Batch import of audio (TASK-163).

## Acceptance criteria
- [ ] «Импортировать запись» in «Записи и задачи» and on a subject: file, then lecture (past lesson, date and time,
      subject); the note «Звук не покидает компьютер, распознаётся локально».
- [ ] The row is marked «импортирована», shows the recognition estimate and the power wait as other recordings.
- [ ] Refusals from the binding (format, size, duplicate) in Russian.
- [ ] Tests; `npm run test` and `npm run build` pass.
