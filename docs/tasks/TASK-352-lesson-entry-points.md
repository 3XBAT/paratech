# TASK-352: Every «Открыть конспект / расшифровку» leads to the lesson screen

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-351
- **PRD sections / AC:** ADR-0034 §2; audit А-1.3, 4.1, 5.1, 5.7
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-352

## Goal
All ways into a lesson end on the same screen, and the old overlays go.

## Allowed to change
- `src/App.tsx`, `src/{openSummary,openTranscript,agentMentions}.ts`, `src/pages/{TodayParts,RecordingRow,RecordingsPage,
  SubjectsParts,TranscriptLinks,HomeworkPage}.tsx`, `src/nav.ts` (remove `transcript`/`summary` from Route), tests

## Out of scope
- Subject tabs (TASK-353), «База знаний» (TASK-354).

## Acceptance criteria
- [ ] «Сегодня»: the whole lesson row opens the lesson (before/while the pair without a recording: «Записать» instead).
- [ ] «Записи» rows, «Пары» of a subject (SubjectsParts lessons, now clickable rows), TranscriptLinks of an assignment,
      agent mention «Пара» and answer sources open the lesson with the right tab and place.
- [ ] `routeToTranscript`/`routeToSummary` and the overlay screens are removed; old links have one replacement each.
- [ ] Deleting the recording on the lesson screen returns to the previous route (not always «Сегодня»).
- [ ] UX check of coordinator-round.md 3a; `npm run test` and `npm run build` pass.
