# TASK-157: Matching imported notes to past lessons (pure domain)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-149
- **PRD sections / AC:** ADR-0027 «Bulk import», Open 2; ADR-0024 (`subjectmatch`), ADR-0022; ARCH-11, ARCH-15, ARCH-17
- **Branch:** ralph/<date>-task-157

## Goal
Given what is known of a file and of the semester, propose a subject and a lecture with a confidence and reasons,
deterministically and without a model.

## Allowed to change
- `paratech/desktop/internal/domain/library/services/notematch/**` (new)
- `paratech/desktop/internal/domain/schedule/services/pastslots/**` (new)
- `docs/architecture.md` §12 (the two service names only)

## Out of scope
- Persistence, use case, UI (TASK-159, TASK-160).

## Acceptance criteria
- [ ] `pastslots.Infer(lessons, semesterStart, today)`: groups current lessons by subject string, weekday, start time
      and kind, detects a weekly or two-week period from the next weeks, projects them back to the semester start
      (Sept 1 / Feb 1 unless given); output marked inferred; never more than 400 slots; holidays not guessed.
- [ ] `notematch.Propose(file facts, candidates)`: subject from folder names, front matter `subject`/`tags`, file
      name tokens via `subjectmatch`; date from front matter, the file name or the first lines (`dd.mm.yyyy`,
      `yyyy-mm-dd`, «12 сентября»); file time is only a weak tie-breaker; lecture = a recording or stored lesson of
      that subject and date, else an inferred slot, else `day`, else `undated`.
- [ ] Confidence `sure` / `likely` / `none` and reason enums (`subject_from_folder`, `date_in_name`, `two_lessons_that_day`,
      …); two candidate lessons on one day is `likely` with both offered.
- [ ] Table tests incl. «ё», aliases, ambiguous subjects, two-week pattern, an Obsidian daily-note name, a file
      name with a command (stays a string).
- [ ] `go vet`, `go test -race` of the touched packages and the architecture test pass.
