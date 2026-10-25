# TASK-106: presentations (part 2: generation through write_work)

Status: stays `pending`: the builder and the generation are done, the UI part of the acceptance criteria is not (frontend follow-up).

## Done
- Part 1 (merged earlier): `values.Presentation`, `adapters/pptx` renderer, 3 templates.
- Part 2: a confirmed work of type `presentation` is written by the `write_work` flow. Outline = slide list (8-12, title first, conclusion last), student approves it, then one slide per queue step (bullets or two columns, <=6 per slide, speaker notes), then one call for 5 likely questions with short answers. Result: `.pptx` (pptx renderer) and a separate speaker-text `.docx` (about 700-1000 words, questions included), both filed into the subject's «ДЗ». Undo of `write_work` moves both back to «Входящие» (refused if the student moved either file elsewhere).
- Hub operation `homework_draft` reused: no Hub, catalog, proto, migration or dependency change. `Writing` got optional fields (`deck`, section `kind`, `bullets`, `columns`); the stored JSON and ContentKey of a paper are unchanged (pinned by a test).
- Model output is untrusted: strict parsers (exact keys, bounds, control and bidi characters removed), re-validated on load; the title-slide subtitle comes from the profile and lesson only; brief and slides go to the writer as data.
- ADR-0025 addendum.

## Decisions to confirm
- Planned notes total is checked as 600-1200 words (prompt asks 700-1000); questions are asked again after any change of the slides.
- Template is always `paper` for now (stored in the deck; picker is a follow-up).
- `work.ErrNotWritable` removed from the domain (no longer used); `TestPresentationIsNotWritten` replaced by `TestPresentationIsWrittenAsSlides`.
- Removed nothing else; `prompts.WorkVersion` bumped to 2 is informational (keys change through the instruction text).

## Follow-ups
- Frontend: Wails `TurnkeyWriting`/`TurnkeySection` do not expose kind, bullets, columns, talk material, questions; the UI still treats a presentation as not writable (`workView.ts`); template picker; slide kind/words bounds (15-250) in the outline editor; second ResultCard for the talk; "Открыть в PowerPoint". Until then the agent's `write_work` can start a presentation the student sees as a plain outline (reviewer medium; nothing unsafe).
- The 5-7 minute length is bounded by the planned words and the prompt; written notes have no lower bound (reviewer medium; same as the paper flow). Possible fix: minimum word count of all notes before the questions step.
- Questions call re-sends assignment and material texts (cost trim).
- Title-slide year comes from the clock at assembly (replay across New Year gives a different file); no case-level EditOutline test for a presentation.
- Template choice on a done work needs a new generation and refiling.

## Changed existing tests
work/writing_test (presentation refusal test replaced), cases/turnkey writing_fakes_test (optional answer hook, fake slides renderer, env wires Slides), tests/turnkey_test (pptx renderer and job-state reader wired).

## Security
security-auditor: no High/Medium, no Low findings (one non-finding note: Undo compares a file's folder with its own subject's «ДЗ»; pre-existing for papers). Reviewer: approve, no blocker/high; 2 mediums (UI missing, talk length) recorded above; no re-review needed.
