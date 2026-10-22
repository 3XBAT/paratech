# TASK-125: cases/todayfeed, posts after each lesson, «Сегодня» data

Done: aggregate domain/agent/post (recording = dedup key), domain service agent/services/today (days, «Горит на 7 дней», card text, chips), use case cases/todayfeed (Poster + Screen.Today/Thread), migration 0024_today_posts (+schema.sql), sqlite TodayPostRepository and ProposalRepository.ListByStatus, Wails binding input/wails/today.go (Today, SubjectPosts), app/today.go hook wrapping the post-processing handler (no completion event existed). No summary post for a lesson without speech; «Да, в календарь» is data only (create_deadline_event, assignment, due).

Review: reviewer M1 (late event of an older transcript flipped the post) fixed in Poster (ignore if not the newest transcript) with a regression test; L2 fixed (detached 5s ctx for the post write). Security auditor: no findings.

## Decisions to confirm
- Post is a new aggregate in the agent context; docs/architecture.md §12 context map needs a line (ADR note in a docs task).
- Posts are stored apart from the thread; subject resolved at read time; not in planner history.
- «Ждёт вашего «Да»» = VALIDATED proposals only.
- Today() fails as a whole if one section fails.

## Follow-ups
- No path yet creates a create_deadline_event proposal from an assignment id (TASK-126 / TASK-127); the button is offered even if such a proposal already exists (reviewer L4).
- Lesson rows have no live recording/recognition status: UI combines with the activity binding.
- Chip facts (todaySubject, summaryYesterday) still computed in the case (reviewer L3); N+1 reads on Thread (L5); no purge of posts after 90 days; no backfill of lost posts (L2 second half).
- TASK-126 must render titles as text.

## Changed existing tests
wails/shell_test.go (validCore gets Today), wails/settings_test.go (missing-today case), sqlite/migrate_test.go (v24, today_posts).
