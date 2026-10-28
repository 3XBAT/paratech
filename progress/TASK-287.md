# TASK-287 — the student's notes rank first; «Мой конспект» is the first tab

## Done
- `knowledgebase.Search` and `Passages` (domain service): inside a lesson the `ownNote` fragments are scanned/added first, then title, summary, lecture text, transcript. On an equal score the note ranks before the AI summary, and the 50-hit (MaxHits) / 12-passage (MaxPassages) caps cut the AI tail, not the notes. ADR-0019 limits, sort keys and determinism unchanged. `cases/answering`, `cases/knowledgebase`, `input/wails` pass the order through: no code change there.
- Frontend: `lessonTabs` is «Мой конспект», «Кратко», «Текст лекции», «Расшифровка», «Задания». The `OwnNotes` block moved out of «Кратко» into its own tab that does not wait for (or show errors of) the summary. `noFocus` is the notes tab, so a lesson opens on «Мой конспект» unless a hit/cite leads elsewhere (summary, lecture text, transcript keep their tabs; an `ownNote` or title hit opens notes). `NoteLecturePage` unchanged.

## Decisions to confirm
- «On an equal match» is read as inside one lesson (the task text says «внутри урока»): across lessons the newer lesson still wins at an equal score, so a newer lesson's AI summary can rank above an older lesson's note. Pinned in `ranking_test.go`.
- A title hit now opens «Мой конспект» (it returns `noFocus`).
- Notes are scanned before the title as well ("scanned first", ADR-0027).

## Follow-ups
- Two new frontend tests grep the TSX source (same pattern as the file's existing ones); they break on a harmless reformat.

## Changed existing tests (the TASK-152/153 pins, flipped)
- `knowledgebase/ownnote_test.go` `TestSearchOwnNotes`: hits were transcript, n1, n2; now n1, n2, transcript (TASK-152 order replaced by ADR-0027 «Ranking»).
- `knowledgebase/passages_notes_test.go` `TestPassagesOwnNotes`: recorded lesson was summary, transcript, note; now note, summary, transcript; field checks moved from `got[2]` to `got[0]` (TASK-153 pin).
- `cases/answering/ownnote_test.go`: `TestAskOrderSummaryBeforeNote` renamed `TestAskOrderNoteBeforeSummary`, expects fragment 0 the note (Note n1, no recording), fragment 1 the summary (TASK-153 pin).
- `frontend/test/knowledge.test.mjs`: the `lessonTabs` labels assertion expects five tabs starting with «Мой конспект».
New: `knowledgebase/ranking_test.go` (equal-score order, cap keeps notes, passages order and cap, across lessons); tab order, default, hit focus and source-guard tests in `knowledge.test.mjs`.

## Verification
go vet and `go test -race` of `domain/coursework/...`, `cases/knowledgebase|answering|tickets`, `input/wails`, `desktop/tests` (architecture), sqlite/app/subjectview/todayfeed/input/agent importers; golangci-lint v2.14.0 on the changed packages: 0 issues. Frontend: npm test 960 pass, build ok.

## Security
Auditor: none found (reorders already-flowing data, no new flow; no innerHTML). Reviewer: approve, no blockers; the missing progress/QA files (medium) are this file and `docs/qa/tasks/TASK-287.md`.
