# TASK-132: turnkey UI - writing progress and result (agent panel)

Status: done for the frontend part; three acceptance buttons are NOT built (no Wails bindings) - see follow-ups.

## Done (frontend only, no Go change)
- `bindings/turnkey.ts`: strict wrapper over TurnkeyService (Work, StartWriting, ApproveOutline, EditOutline, Regenerate); core error texts shown as fixed Russian strings.
- `work` agent card (workId, outcome) is now parsed (`bindings/agent.ts`, `agentView.ts`) and rendered by `agent/WorkCard.tsx` with the pure model in `workView.ts`:
  - confirmed, no writing: «Написать работу» (hidden for presentation);
  - outline/section/assemble: progress «Раздел N из M», polling Work() every 3 s (15 s after a failed read, stops on not-found); Writing.Material never shown while running;
  - approve: «План готов, утвердите» - outline editor (edit, add, remove, move), «Утвердить план» (EditOutline if edited, then ApproveOutline), «Сохранить план»; limits equal the domain (12 sections, 50..3000 words, 15000 total, title 150 runes, note 1000);
  - blocked: «Сгенерировать заново» (section 0 only for a blocked outline, else the first unwritten section);
  - done: result card (.docx line), «Доработать» (section + note), «Изменить план» + «Написать заново», sections in `<details>`, sources with «проверить источник», «Сгенерировано — прочитайте перед сдачей».
- Model text is plain React text (no HTML, links, markdown); nothing stored in the browser; stale Work() replies dropped by version; single-flight guard on buttons; action error lives in the parent so a remount does not lose it, with «Повторить» (StartWriting).

## Decisions to confirm
- «Открыть в Word», «Остановить», «Отправить преподавателю» are not built: no binding opens a library material, cancels a writing job, or prepares a send of a material (HomeworkService.PrepareSend works on an assignment draft). Acceptance criteria of TASK-132 are therefore only partly met.
- TASK-130 (questions / «Как я понял задание» / helper outcomes of the work card) is not part of this; those outcomes render nothing. The AnswerService answer card is not built (not in this task).

## Follow-ups
- Go task: bindings for open-in-Word (material id), cancel writing job, send the paper to the teacher (via the confirm card of TASK-128), and expose in TurnkeyWork whether a writing job is active (today a stopped job shows as running forever; the UI can only offer «Повторить» after an error), and the section being written (progress line uses done+1).
- Several `work` cards for the same workId each mount their own poller/editor; EditOutline carries no version (stale editor can overwrite). Show the editor only on the latest card or add an expected version.
- No component-level test of WorkWritingCard (helpers and static renders are tested).
- Reviewer: the last fix (H1, error kept in parent + «Повторить») was not re-reviewed by a third pass.

## Changed existing tests
None (tsconfig.test.json lists the new sources).

## Security
security-auditor: no findings. Reviewer: 2 High (client limits vs domain, blocked regenerate rewriting all sections) fixed and re-reviewed; one High (error lost on remount) fixed afterwards, not re-reviewed.
