# TASK-107: lab report (write_work, lab flow)

Status: stays `pending`: the core is done; PDF text and the UI part of the acceptance criteria are not.

## Done
- A confirmed `lab_report` work gets an optional `Writing.Lab` (manual state) and sections an optional `Part` (goal, equipment, theory, procedure, results, conclusions, other). No migration; a plain paper's stored JSON and ContentKey are unchanged (tests).
- Prompts: `lab_outline` / `lab_section` on `homework_draft` (WorkVersion 3, no Hub change): structure from the manual if there is one, else the standard six parts; never invent data; placeholders of the exact form «▢ Вставьте …»; conclusions conditional on the student's data; brief, assignment and manual are data.
- Strict parsing: results section without a placeholder is refused (uses an attempt); placeholder one line, <=200 runes, <=20 per section, bidi/control dropped; re-validated on load.
- .docx by the app: highlighted placeholders, note under the conclusions, final block «Что доделать самому», note when the manual was not read.
- Manual state (`read`, `none`, `not_read` for PDF, `no_text`) goes to the writer and into the paper. DOCX/TXT via existing doctext (local, no CGO).
- Injection tests (hostile manual stays in the data block, no action changes). ADR-0025 addendum.

## Decisions to confirm
- A lab report started from a material file itself: readable file = manual in assignment_text (`read`); no text/PDF/removed = the job fails (ErrWriteGone), because the domain needs an assignment text and StartWriting reads the source.
- Title page reads «Отчёт по лабораторной по дисциплине …» (TypeName); «…лабораторной работе» may read better.
- "Attach files to the assignment" is the assignment's existing material only; a separate attach action is not built (owner question, linked to ADR-0027).

## Unmet acceptance
- PDF text extraction: not done (no dependency; ADR-0027 / TASK-156). PDF gives the honest "not read" message.
- UI: Wails DTO does not expose Lab, Manual, Placeholders, section Part.

## Follow-ups
- Frontend: show placeholders, to-do list, manual-not-read message, part picker in the outline editor.
- Medium (reviewer): the ADR line "shown in the view" is true for the use-case view only until the Wails DTO is extended.
- Re-review after the medium fixes was not run (fixes were small, tests and lint green).

## Changed existing tests
None changed in meaning (internal signatures only); the new TestLabManualNotRead was corrected during the work.

## Security
security-auditor: no High/Medium. Reviewer: approve, 3 mediums (UI gap, manual-as-source, docs) addressed or recorded above; 4 lows fixed (todoLine, neutral no-text wording, no citation of an unread manual).
