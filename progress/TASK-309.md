# TASK-309 — schedulesubjects domain service and subject.RemoveAlias

## Done
- New pure package `domain/library/services/schedulesubjects` (std, `values`, `subjectmatch`, `subject`, `shared`; no time/IO/randomness): `Propose(rows, subjects)` -> `Proposal` of groups; `Apply(proposal, choices)` -> `Outcome` (Creates, Additions); `DeriveSubjectID(token, key)` (`sched-` + 32 hex of sha256 over length-prefixed token and key).
- Lesson-kind marks: ONE table (`marks.go`), cut at the start or end, in brackets, after a separator (`, - – — : /`); plain space + mark at the end cuts only abbreviations; two-letter ones (`пр`, `пз`, `лр`) need a dot. «Учебная практика», «Семинар по физике» stay as they are. Kind-less lessons of a marked row count as the mark's kind.
- Obvious group = same key after the cut; name = most used cleaned spelling; raw spellings are aliases (not those with the name's key); kinds = union.
- Doubtful: abbreviation (initials, «и» skippable; or prefixes >= 2 letters per word, ≥ 2 significant words) and typo (Levenshtein 1 for 5-8 runes, 2 for longer; guards: same word count, first 3 letters of each word, equal numerals incl. Roman). Merged by default (`Doubtful`), member choice «Отдельно» / «Не заводить». Two rounds (typos, then abbreviations), a string joins only a lead and only when exactly one lead fits (no chains). Same rules against names and aliases of existing subjects; one match -> add to that subject; ambiguous or already matched strings are left out.
- `Apply`: defaults when no choice; renames checked with `subject.NewName`; unknown group/member, bad action, duplicate choice -> sentinels (no schedule text in any error); `ErrNameTaken` for shared spellings; `ErrProposalNotBuilt` for a hand-built proposal. Deterministic order (lessons + recordings desc, then name).
- `subject.RemoveAlias(alias, now) bool` (by key, never the name, `updatedAt` untouched when nothing changed).
- `docs/architecture.md` §12: `schedulesubjects` added to the library row.
- Agent: `propose_subjects_from_schedule`, class C, wave 10 (TASK-319, TASK-323).

## Decisions to confirm
- `Propose` takes `[]subjectmatch.Entry` (not an `Index`): `Index` has no exported fields, so names and aliases could not be compared for abbreviations and typos. `subjectmatch` untouched.
- The package imports `library/subject` (for `subject.NewName`, `MaxAliases`): ARCH-10, same context.
- Review fixes: abbreviation matching was exponential on crafted strings (HIGH) -> shared DP `matchWords`, regression test with a 5 s bound; `Create.Aliases` capped at `subject.MaxAliases` (spellings beyond it are dropped but their counts stay); `subjectKeysOf` unions keys of a repeated id like `subjectmatch.NewIndex`.
- Owner question: the typo rule as specified merges «Философия» / «Филология» (distance 2, 9 letters) and «Этология» / «Этнология» by default (doubtful, the student can split). Tighten (distance 2 only from 12 letters, or no double substitutions)?

## Follow-ups
- TASK-310: re-run `Propose` with the current subjects right before `Apply` (a proposal not built by `Propose` is refused); handle `subject.ErrTooMany` on `Additions`; cap the number of distinct rows given to `Propose` (worst case is polynomial, O(rows² · 200²)).
- Dropped spellings beyond the alias cap attach to the new subject on the next `Propose` as an Addition (ErrTooMany there).
- `Group`/`Member` exported fields of a built proposal can be edited by a caller; `Apply` does not re-read them from an unexported copy.
- Hyphen marks without a space («Физика-практика») are cut by design; a rare real name like «Учебно-лабораторная …» would lose its tail.
- Optional: precompute words/compact per cluster.

## Changed existing tests
None (`TestRemoveAlias` added to `subject_test.go`).

## Security
Auditor: no High/Medium; one CPU-exhaustion finding (exponential backtracking in the abbreviation matcher), fixed with a DP and a regression test; the reviewer re-checked the fix (approve). ID derivation, determinism, error text (no schedule strings) and purity found clean.
