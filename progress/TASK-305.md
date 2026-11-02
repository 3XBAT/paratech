# TASK-305 — extraction: a deadline that names only a day is 20:00 (prompts v6)

## Done
- Owner decision C-5 = (а) 2026-11-02: extracted assignments get 20:00 as well, same as the student's date-only deadline (TASK-304).
- `domain/coursework/services/prompts/prompts.go`: the extraction rule «If only a day is named, use 23:59 of that day» -> «use 20:00 of that day»; no other prompt wording changed. `Version` 5 -> 6 with a comment (ADR-0029 addendum 3).
- prompteval fixtures: the expected deadline T23:59 -> T20:00 where the lecture names only a day (02-lecture-dates: «до понедельника», «до пятого ноября»; 03-seminar-subgroups: «до двадцатого октября»; 07-injection: «до пятницы»). Next-class and explicit-time cases untouched.
- Hub/shared: the Hub only routes the task kind `EXTRACT_HOMEWORK_DEADLINES`; no copy of the extraction prompt and no check of the desktop prompt version there. Nothing to deploy first. The Hub agent-panel prompt/tools wording is out of scope (TASK-244).
- ADR-0029 addendum 3: the open item C-5 closed with the answer.

## What the version change does on reprocessing (read from code, rules unchanged)
- `prompts.Version` is in the post-process job key (`homeworkflow/postprocess.go` `jobKey`), the LLM key (`llmKey`), the material job key (`material.go`) and the draft key (`drafts.go`). A new transcript or material gets v6 jobs; the shared constant also moves summary/draft keys (expected under «bump on any change»).
- No startup backfill: old transcripts are processed again only through «Повторить» / «Сделать конспект заново» (Rerunner) or a new transcript. Stored deadlines are never rewritten by the bump itself.
- On such a rerun `storeAssignment` finds the same source item and calls `Assignment.Reextract(d, 6, now)`: user_edited, done and deleted assignments are not changed; others take the new details (a day-only deadline may move 23:59 -> 20:00) and PromptVersion 6. Assignments absent from the new answer are withdrawn (Superseded) unless user-edited, done, deleted or used by a delivery. `SetHints` updates hints on user-edited ones but skips deleted.

## Decisions to confirm
- None. Stored 23:59 deadlines are left as they are (ADR-0029 addendum 3: no migration).

## Follow-ups
- A live prompteval against the real Hub/LLM was not run (fake only); the owner may run it to see the model really answers 20:00.

## Changed existing tests
- prompteval fixtures 02, 03, 07 (expected deadline T20:00) and the fake partial answer for lecture-dates in `desktop/tests/prompteval_hub_test.go` (T20:00 so it matches the fixture). No version literal changed: all tests use `prompts.Version`.
- New: `TestExtractionDayOnlyDeadlineIs2000` in `prompts_test.go` (Version 6, new sentence present, no «23:59»).

## Security
Prompt wording and fixtures only. security-auditor: no findings (LLM output path unchanged and still a proposal for the student; no blind resend; user-edited data kept). Reviewer: approve; its two items (this file, ADR open item) done here.
