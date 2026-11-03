# TASK-289 — subjects: «Удалить предмет» and «Вернуть» (binding + UI)

## Done
- Go (`input/wails`): `DiscardSubject(id, version)` -> `{problem, notes, files, version}` (problem `changed|not_found|failed`), `RestoreSubject(id, version)` -> `{problem, subject}` (problem `expired|changed|not_found|failed`). `CreateSubject` with the name of a removed subject returns the typed error `LibraryRemovedNameError{problem:"removed_name_taken", subjectId, version}` (message and `errors.Is` as the plain name-taken, so old callers behave as before; Wails passes it as `err.cause`, pinned by a test). `ScheduleSkippedRow` gains `removedSubjectId` and `removedSubjectVersion` (cleared when the subject is no longer removed).
- `cases/library`: new read `Service.RemovedSubject(ctx, id)` (outside the listed files): returns a removed subject only, to give the binding the version for `RestoreSubject`; keeps `RemovedNameError`/`ScheduleSkipped` of TASK-288 untouched.
- Frontend: native `<dialog>` with the warning (counts from the page: own notes, files not parts of notes; re-read on open so the version is fresh), refusals with a way out, line «Предмет «X» удалён» + «Вернуть» (restores and opens the subject), `removed_name_taken` line with «Вернуть» in the create form and the inbox place panel, «Вернуть» on skipped schedule rows. Logic is React-free in `subjectRemovalView.ts`, hook `useSubjectRemoval.ts`, components `SubjectRemoval.tsx`.
- Review (opus): round 1 changes requested (HIGH stale version after createFolder; mediums: changed text/button, restore refusals without a button, misleading «уже есть», nested live region, loading counts); UX expert: must-fix items done (glossary «мой конспект», what stays, restore selects the subject, secondary button, aria); round 2 approve.

## Decisions to confirm
- Busy label «Возврат…» (existing impersonal `changeTexts.restoring`), not «Возвращаю…».
- Verb «удалится» for exactly one part with a singular number, «удалятся» otherwise.
- «мой конспект» in prose («2 моих конспекта») follows the glossary label but is first person; alternative: quote the label.
- Warning always adds «Записи пар и задания останутся.» (ADR-0027).
- Журнал: no «Вернуть» for deleted assignments in `journalView.ts`, so no Журнал entry.
- The «Вернуть» line is not persisted: after leaving the page the only way back is creating a subject with the same name.

## Follow-ups
- **«Недавно удалённые» list** (UX expert: critical for a 3-day promise): needs a core read of removed subjects with their purge date, a tree entry, and a merged line for several removals.
- Purge vs held name: after 3 days and before the purge (TASK-308) creating the name still answers «удалён» and «Вернуть» says «удалён окончательно»; the core should free the name of an expired subject (or purge on reuse).
- `materialFlow.placeHint` "create" says «Предмет с таким названием удалён.» but offers no «Вернуть»; `removedNameText` is mapped to «Обновить список» which does not free the name. `AddAlias` with a removed name still gives the plain text.
- «⋯» menu of the subject head for «Удалить предмет» (rename/aliases later); empty subject could be removed without a dialog (task text keeps the question).
- Reviewer lows: «Повторить» (re-create) on `changed` in the schedule review duplicates the notice's «Обновить» and may create a new subject if the removed one was purged; `NameForm` always renders a wrapper (extra 8 px gap); aria-label hides the busy state; the line after «Удалить» loses focus on «Закрыть» and has no name aria-label; «Название можно ввести ещё раз» on the top line does not say where; punctuation of sibling texts is inconsistent.
- Tickets of a removed subject are still not filtered (TASK-288 follow-up). Recordings stay in the knowledge base under the bare subject string during the window (ADR-0027).

## Changed existing tests
- `test/library.test.mjs`: tree row markup gained `data-subject-id`.
- `test/subjectsHome341.test.mjs`: one more shared slot (`removalBlock`) before the views; the review is still not remounted.
- `test/libraryInbox.test.mjs`: `removedNameText` in the way table (as refresh) and the known texts.
- `test/materialFlow.test.mjs`: the fake `createSubject` accepts a cause; one case checks the «удалён» hint.
- Go: none (only a struct gained fields in `library_test.go` fakes).

## Security
security-auditor (opus): no findings (destructive calls only from a click in the dialog, restore only from a click, version checks, names as text nodes, no leaks, new read only for removed subjects).

## Verification
go vet and GOOS=windows go vet ./desktop/...; go test -race on input/wails, cases/library, input/agent, app, tests (architecture); golangci-lint v2.14.0; npm run test (1616) and build.
