# TASK-353 — страница предмета: вкладки «Пары · Файлы · Задания», «Билеты к экзамену»

## Done
- **Go (small read, added by the coordinator's decision, see below):** `overview.Open` (pure domain: all open assignments of the subject, dated first by deadline with overdue included, undated last, ties by id, no cap), `subjectview.Overview.Assignments` (same resolved list as «Ближайшее», one resolver call), wails `SubjectOverview.assignments` (`assignmentId, title, due, daysLeft`). `Nearest` unchanged. Table/service/DTO tests.
- **Route:** `Route.subjectTab` ("" | pairs | files | tasks); `routeToSubjectTab`, `routeToTickets(r, subjectId)` (page tickets + `subject`). A tab switch is not a history entry (replaces the tab of the entry); a lesson/assignment/tickets opened from a tab is, so Alt+← returns to that tab. The default tab is decided once per opened subject after the lessons read and written into the route.
- **Page:** `SubjectHead` (name, sub-lines, «Билеты к экзамену», head «⋯» with «Удалить предмет»), teachers, `SubjectTabs` (SegmentTabs, tabpanels, counts), `SubjectPairsTab` (lessons by `subjectId` via `subjects()`→`lectures(key)`, row opens the lesson screen, import, past note, export in the tab «⋯», status chip only for «В обработке»/«Сбой обработки» + «новое» joined from `overview.lessons`), `SubjectTasksTab` (rows by deadline, «Добавить задание» with `AddAssignmentDialog initialSubjectId`, re-read error with «Повторить»), «Файлы» = `SubjectDetail` + «Добавить файл». Folder click in the tree switches to «Файлы» and scrolls. Focus fix of a folder's «+» (`useFocusBack`, `folderAddId`).
- Tickets: opened for the subject, title/crumb «Билеты к экзамену», crumbs `Предметы / <предмет> / …` (closes the TASK-350 follow-up), subject select hidden behind «Предмет: … Сменить».
- «Билеты» button left the heads; «База знаний» button moved to the tree actions (the search stays reachable until TASK-354).
- Quiet loading: `«Загрузка…»` after 800 ms (`useAfterDelay`), stable panel height, first tab tabbable while none is selected (`SegmentTabs`).

## Decisions to confirm
- **Go added although the task says «только UI»:** «Задания» needs ALL open assignments of a subject (overdue and undated too); the overview had only the 5 nearest upcoming. Resolution to a library subject is a core rule (`ResolveSubjects`), so it was not copied into the UI. Reviewed by security-auditor: nothing found.
- Files touched outside the task list: the three Go packages, `bindings/library.ts`, `navHistory.ts`, `HomeworkDialogs.tsx` (prop), `ui/Button.tsx` (`IconButton id`), `ui/RowMenu.tsx` (item `title`), `ui/controls.tsx` (tab stop), `SubjectRemoval.tsx`, `RecordingImportPanel.tsx` (`onBusyChange`), `pages/tickets.css`, new files listed in the commit.
- «Пары · N» counts all rows of the list (notes without a recording included).
- A subject without lessons and notes has no knowledge-base group: «Добавить конспект прошедшей пары» uses a stand-in key `library:<id>` that never leaves the frontend (security-auditor checked: notes are filed by the library subject id).
- Status chips on «Пары» come from `overview.lessons` (the 10 latest); older rows have none.
- Switching tabs adds no history entry (as lesson tabs).
- The subject crumb from Tickets opens the subject on its default tab (Alt+← restores the tab).
- «Билеты» select change inside the page does not change the route.

## Follow-ups
- Drop `overview.deadlines` and `overview.lessons`-heavy reads from `subjectview` if the status chips go too (the UI no longer reads `deadlines`); a failure in those reads fails the whole overview (reviewer).
- The assignment list of a subject is cut by the global `MaxListedAssignments` (newest first): very old open assignments of a subject can be missing.
- Counter on «Файлы» / «Все задания предмета» link (closed and sent assignments are unreachable from the subject page).
- Tab «Задания» count can arrive after the «Пары» count (separate reads).
- «Повторить» after a failed re-read has no busy feedback.
- `KnowledgeBasePage.SubjectView` still has its own copy of the lessons list/export (TASK-354 removes it).
- Check on screen: focus after the folder «+», focus ring on the tabs and «⋯», the «База знаний» button in the 220 px tree, wrapping of the Pairs button row, light/dark chips.

## Changed existing tests
Route shape (`subjectTab`): lessonRoute351, lessonEntry352, summary, transcript, rail348. breadcrumbs350 (Tickets crumbs and `SubjectHead` sub-line). quiet347 and lessonEntry352 (rows of «Ближайшее»/«Пары» moved to the tabs; `LessonList` now also gets `extra`). subjectpage227 (tests of the removed `OverviewSections`, `lessonTitle/lessonMeta/durationText` removed with the code; `assignments` fixture). subjectremoval289 (`RemoveSubjectButton` replaced by the «⋯» menu item). libraryUx339, subjectsHome341 («База знаний»/«Билеты» not in `LibraryActions`). teachers238 (order head → teachers → tabs). words344 (word taken from `tabTexts`). Go: `TestOverviewNoScheduleNoDeadlines`, `TestSubjectOverviewDTO`, `TestSubjectOverviewEmptyListsAreArrays` (new `assignments`). No assertion weakened; new: `test/subjectTabs353.test.mjs`, Go tests for `Open`, `Assignments`, DTO.

## Review
Reviewer (opus): changes requested (silent failed re-read, dead texts) -> fixed; re-review: approve. ux-expert (opus): «доработать» (pair status/«новое» lost, loading flash, vague overview error, export wording) -> fixed; second pass «доработать» (flash on Back, duplicated chips, wrong re-read text) -> fixed with a small round; not re-run after that (checked by the coordinator against the list). security-auditor (opus): nothing found.
