# TASK-352 — все входы в пару ведут на один экран пары

## Done
- **Go (prerequisite, supervisor scope):** `KnowledgeBaseService.LessonByKey(key)` opens any lesson by key (`recording:/lesson:/day:/note:`), so cold links and history work for note-only lectures. Files: `domain/coursework/services/knowledgebase/notes.go` (`ParseKey`, `FindByKey`, `MaxKeyBytes` 200), `cases/knowledgebase/{service,errors}.go` (`LessonByKey`), `input/wails/knowledgebase.go`, tests in the same packages, `docs/agent-parity.md` (row «Предметы и лекции базы»). No new error or page text: not found = `ErrRecordingNotFound`, bad key = `ErrInvalidRequest`. A `recording:` key returns the lesson without notes (the lesson screen reads notes itself).
- **Frontend:** `routeToSummary/routeToTranscript`, `Route.summary/transcript`, `SummaryPage`, `openSummary.ts`, `openTranscript.ts` are gone; `openLesson.ts` (`useOpenLesson`) is the one opener. `LessonScreen` loads by `lessonByKey` when the lesson is not in the window's cache.
  - «Сегодня»: the pair title is a button stretched over the whole row; «Записать» only from 10 min before the start to the end (no end: start + 90 min); a finished pair without a recording is a plain row.
  - «Записи»: one «Открыть» (no tab: the screen picks `openingTab`), the «⋯» keeps «Открыть конспект / текст лекции / расшифровку».
  - Subject «Пары» rows are buttons; assignment `TranscriptLinks` = one «Открыть пару» per recording (`pairLinks`); agent answer sources open the lesson on the right tab and place; «Пара» pill resolves the pair by `scheduleLessonId`.
  - «Удалить запись» is in the head «⋯» of the lesson (it went away with `SummaryPage`); after it the window returns to the previous page and the deleted lesson is dropped from the history (Alt+← does not return to it).
  - A lesson appears once on «Сегодня»: «Итоги дня» skips lessons that are pair rows (by what `lessonOpen` returns); a post with a calendar question stays.
  - Lesson-screen errors: «Этой пары больше нет: запись или файлы удалены.» («Назад») and «Не удалось открыть пару.» («Повторить», «Назад»).

## Decisions to confirm
- «Записать» window: 10 min before start .. end; it opens the generic start panel (the core call takes only the source, so the pair is not preselected).
- Delete moved from a visible button of the old summary page into the lesson head «⋯».
- Plain status chips are under the stretched-row overlay: a click opens the lesson, their tooltip (failure details) no longer shows on rows with «Открыть»; the reason stays on the lesson screen.
- Touched outside the file list: `SubjectsPage`, `TodayPage`, `todayView`, `recordingRowView`, `knowledgeView`, `KnowledgeLessonPage`, `LessonGone`, `TranscriptPage`, `SummaryParts`, `summaryView`, `navHistory`, `bindings/knowledgeBase.ts`, `styles.css`, `tsconfig.test.json`, `settings_test.go` (bound method count 7 -> 8).

## Follow-ups
- Agent «Пара» pill and note sources still go through «База знаний» (switches the section, refusal is a banner there); better: resolve in place, `LessonByKey("lesson:"+id)`.
- A finished pair without a recording or own files is a plain row (needs a core call that opens an empty lesson of a schedule lesson).
- Low (re-review): stale `refocusSheet` flag when the window already left the lesson; focus after Alt+← out of a lesson stays on body; no loading line during «Повторить» of a cached lesson; chip tooltips lost under the overlay (raise `.chip[title]`?); QA step 8 does not say how to reach the two error texts; `canGoBack =(h` formatting in navHistory.ts; stale doc comment of `rowLessonTab`.
- Preselect the pair in the start panel (needs a Go change in `startRecording`).

## Changed existing tests
Route shape / removed overlays: lessonRoute351, summary.test, transcript.test, navhistory.test, rail348, breadcrumbs350; main button «Открыть» with `onOpenLesson`/`lesson` key and the longer «⋯» menu: lecturetext, recordingrow, summary; today.test (stretched title button, aria-label); agentmentionsui (pill wording, `mentionOpening("lesson")`); quiet347 (`.drow .meta` rule, `onOpenLesson` prop); notelectures155 (banned-string name); agentconfirm (prop name); Go: `fakeKnowledge` got `LessonByKey` (interface), `settings_test.go` method count. Nothing weakened; assertions of removed code were replaced by behavioural ones. New: test/lessonEntry352.test.mjs, Go tests for ParseKey/FindByKey/LessonByKey and the binding.

## Review
Reviewer (opus): changes requested (record window on every future pair, QA step 8, chip dead zones, tab inconsistency, duplicate in «Итоги дня», history after delete, source-regex tests) -> fixed; re-review: approve. ux-expert (opus): «доработать» x5 (agent pill detour, finished pair row, error texts, chips, tabs) + record window -> fixed except the two follow-ups above. Security: security-auditor (opus) on `LessonByKey` and the delete menu — nothing found (read-only binding, key only compared with core-built keys, size-limited, fixed error texts, existing delete dialog).
