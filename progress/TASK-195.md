# TASK-195 — work card for presentations and lab reports, «Замечания»

## Done
- bindings/turnkey.ts: strict parsers for the TASK-194 fields (missing = empty/false, wrong type or unknown kind/part/manual throws, manual "" accepted).
- «Написать работу» is offered for presentations; progress «Слайд N из M».
- Plan editor modes (paper / presentation / lab) with the domain bounds; fixed title and conclusion slides; last results/conclusions section is not removable.
- Ready presentation: slide list in `<details>`, «Вероятные вопросы», «Открыть в PowerPoint», «Открыть текст выступления» (openMaterial by id, copy caption).
- Ready lab report: part labels, highlighted «▢» lines, «Что доделать самому», manual not_read / no_text lines.
- «Замечания» for any ready work through the existing regenerate(id, 0|N, note); empty note sends nothing; singleFlight.
- Files: src/bindings/turnkey.ts, src/workView.ts, src/agent/WorkCard.tsx, src/agent/agent.css (3 rules), test/work.test.mjs, test/turnkey.test.mjs.

## Changed existing tests
- work.test.mjs: presentation now offers «Написать работу»; reviseOptions expects «Вся работа» / «Раздел N. …»; fixtures got the new default fields.
- turnkey.test.mjs (outside the task file list): one assertion asserted the removed behaviour (confirmed presentation renders ""), now expects «Написать работу».

## Decisions to confirm
- The editor does not send slide kind/part; the core restores them.
- Placeholders are paragraphs starting with «▢» (as in domain lab.go).

## Follow-ups (reviewer lows)
- Slide heading and «Слайд N.» option use the stable section number, which can differ from the position after the student adds/moves slides; show position, send s.number.
- `openLabel` checks only workType === "presentation", the file type also checks writing.presentation; unify via editorMode.
- Presentation editor still says «раздел» for the add button and title field.

## Security
Auditor: no findings. Model text is rendered as text nodes only; files open by id; note passes as data via the existing Regenerate path.
