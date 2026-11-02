# TASK-341 — «Предметы» opens the tree and a subject right away

## Done
- Frontend only. The tab opens tree (left) + subject (right) at once: the last opened subject (localStorage `paratech.subjects.last`, try/catch, validated) else the first of the list. Route "" = «no choice yet», resolved once per rail click (`landingFor`); an unknown id (deleted subject) shows the home subject, no redirect and no history push. Pure logic in `src/subjectsHome.ts`.
- «Входящие» is the first tree item. With files: bold name + count pill, `aria-label`/`title` «Входящие · N»; without: quiet. Unknown state (loading / failed) shows neither count nor quiet look. Its view is the TASK-339 list; a failed load shows an ErrorLine «Не удалось загрузить «Входящие»» with «Повторить» (also on the no-subjects screen).
- Tiles only when there are no subjects («Завести предметы из расписания», «Добавить предмет»); the head there has no «Предмет»/«База знаний»/«Билеты». One shared return for both views keeps `ScheduleSubjectsReview` mounted when the first subjects are created (plus `key`).
- «Предмет», «Папка» under the tree; «Добавить файл», «База знаний», «Билеты» in the head of the right part; «Добавить файл» inside a subject puts the file into that subject.
- States: no per-folder «empty» text or «—» count while a subject's files load; one ErrorLine «Не удалось загрузить файлы предмета» + «Повторить» on failure.
- Tree is sticky (own scroll when tall; static below 900px); the drag cue is a reserved line above the items, only in the «Входящие» view; forced-colors outlines; wording «на предмет слева».
- Tests: `test/subjectsHome341.test.mjs` (new), 1512 frontend tests pass, `npm run build` green.

## Decisions to confirm
- The drag cue / hints say «слева» (wrong in the narrow layout, <=900px, where the tree is above).
- A rail click while a subject is open adds a "" history entry showing the same subject (Back then looks like a no-op once).
- «Билеты»/«База знаний» stay in every subject's head (they open the global pages).

## Follow-ups
- Move «База знаний»/«Билеты» out of the subject head (they read as per-subject); or open them filtered.
- A file dropped on the window should follow the view (subject -> «Материалы»); now it goes to «Входящие».
- Inline subject name field in the tree instead of a form far from the button; arrow keys/Home/End in the tree; narrow-window layout of the head; an inbox icon; expand a collapsed semester on drag hover; «Повторить» without feedback while the request runs.
- `addSubjectVariant` in `scheduleSubjectsView.ts` is now used only by tests (remove with them).

## Changed existing tests
- `library.test.mjs`, `scheduleSubjects312.test.mjs`: the tile list page (`SubjectList`, `subj-card`) is gone; the tests use `SubjectTree` / `EmptySubjects`, the loading test checks the small «Загрузка…» line.
- `libraryInbox.test.mjs`: grip tooltip wording («слева»), drag cue now on the tree (above the items).
- `libraryUx339.test.mjs`: tests of the removed scroll-to-inbox helpers replaced by tests of both `InboxList` forms and the «Добавить файл» tooltips; wording updates.
- `navhistory.test.mjs`: title and a comment only.

## Security
Frontend view/CSS only; one new localStorage key, validated and used only to match a listed id. security-auditor: no findings (names are text nodes only; the stored id only matches a listed subject; no new bindings). Reviewer round 1: one high (review panel remount) and mediums (history trap, inbox error shown as empty, loading shown as empty) fixed; round 2 approve. ux-expert round 1 «доработать» (4 must-fix) fixed; round 2 three small items fixed (no per-folder loading lines, inbox error on the empty screen, cue only in inbox view) plus focus-ring room; not re-run after that.
