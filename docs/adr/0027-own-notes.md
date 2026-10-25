# ADR-0027: The student's own notes («Мой конспект») for lectures

- **Status:** Proposed
- **Date:** 2026-10-25
- **Source:** owner 2026-10-25 (a student starts mid-semester: notes exist handwritten, typed, in files, some
  lectures recorded elsewhere); ADR-0017, ADR-0018, ADR-0019, ADR-0022, ADR-0024, ADR-0025 (tickets); TASK-118,
  TASK-122, TASK-133, TASK-138. Tasks TASK-149…169.

## Context
Today a lecture exists in paratech only as a recording: «База знаний», answers with citations (a cite points at a
summary section, a lecture text block or transcript segments of a recording) and ticket coverage read recordings only.
The library (ADR-0024) stores files by sha256 and links them to schedule lessons, but their text is not searched or
cited. The ICS import starts yesterday (`icsschedule.WindowPast`), so for a student who starts in October the past
lessons are not in the schedule at all. The UI already calls the AI summary «Конспект».

## Decision

### Model (desktop, context `library`)
- **Aggregate `library/note`** (`shared.NoteID`): subject (required), anchor, title (student's text or the file name,
  display only, ≤ 120 characters), ordered parts (1…200), status active/removed, version. A part is a library material
  (ADR-0024: blob by sha256, detected type, soft delete). Pasted text becomes a TXT material (≤ 1 MiB). Provenance is
  always «ваш конспект»; nothing generated is ever stored in a note.
- **Anchor** (one of): `recording` (a recorded lecture), `lesson` (a schedule lesson, past or future, without a
  recording), `day` (subject + local date, «пара 12 сентября» with no schedule lesson), `undated` («лекция без даты»,
  ordered by the student). Every anchor also stores a snapshot (date, lesson kind, title) so a deleted recording or a
  re-imported schedule falls back to `day` at read time and never orphans or deletes the student's text (no FK from
  a note to recordings or lessons; the ADR-0018 purge is unchanged).
- **Filing**: a part that arrives in «Входящие» is placed in the subject's system folder «Лекции» («Семинары» /
  «Лабораторные» by lesson kind); a material already filed elsewhere stays where it is and is only referenced.
- **Use case `cases/notes`** (add files, paste, replace a part, reorder, re-anchor, remove/restore); materials through a
  port implemented by `library.Service` (wired in `app`, no use case imports another, ARCH-20).

### Text
- `material.Text` gets `LocatorSection` (a heading of Markdown or a Heading style of DOCX; the heading is the first
  line of the fragment). Markdown is still detected as TXT by content; a light reader in the domain drops YAML front
  matter (its `date`/`subject`/`tags` become import hints only), turns `[[x|y]]` into `y`, drops embeds and HTML. Notes
  are shown as plain text, never rendered as HTML.
- PDF text (postponed by ADR-0024) gets a pure-Go reader in `adapters/doctext` (dependency chosen and recorded by
  TASK-156); a PDF without a text layer is a scan (see Handwriting).
- The extracted text of a note part is cached as a derived artifact by its digest (rebuildable, removed with the blob),
  so a question does not unzip every DOCX again (ADR-0019 stays: no index).

### Knowledge base, answers, tickets
- A KB lesson is a recording or a note-only lecture (`lesson`/`day`/`undated` anchor); notes with a `recording`
  anchor, or a `lesson` anchor whose lesson has a recording, join that recording's lesson.
- Search and `knowledgebase.Passages` take note fragments; `answer.Source` gets section `ownNote` with note, material
  and locator (page / slide / section / fragment) and an empty recording. The card shows «ваш конспект · стр. 3» or
  «ваш конспект · раздел „Ряды“». Equal match: the student's fragment ranks before the AI summary.
- Tickets (ADR-0025) use `answering.Ask`, so notes count in coverage with no change of the rule; «нет в ваших
  лекциях» reads «нет в ваших лекциях и конспектах».
- **Coexistence**: the AI summary and lecture text of a recorded lecture are never merged into or written over the
  student's text, and the reverse. The lesson page shows «Мой конспект» and «Конспект» side by side with provenance.

### Bulk import (mid-semester start)
- **Aggregate `library/importbatch`**: up to 500 items (digest, size, type, display name, relative folder path,
  hints), a proposal per item, the student's choice per item, outcome; draft → committing → done | cancelled. It is
  persisted, so the review survives a restart. Same pattern as the schedule import (PRD §11.2): preview, fix, commit.
- **Sources**: multi-select open dialog, a folder (Obsidian vault, a Notion export unzipped), a `.zip` (Notion
  export), drag-and-drop (TASK-138 allow-list). The walker (`adapters/notesource`) skips hidden and `.obsidian`/
  `.trash` folders, follows no reparse points, and has depth/count/size limits; the vault is read once and never
  written (no sync).
- **Matching is deterministic and local** (`library/services/notematch`, pure): subject from the folder names, front
  matter or file name through `subjectmatch`; date from front matter, the file name or the first lines (dd.mm.yyyy,
  «12 сентября»), never from the file time alone; then the lesson: a recording or a stored lesson of that subject and
  date, else a slot inferred by `schedule/services/pastslots` (the weekly or two-week pattern of the current lessons
  projected back to the semester start; shown as «предполагаемая пара», never written to the schedule or Google).
  Each proposal carries a confidence (sure / likely / none) and reason enums; nothing is sent to a model.
- **Commit** is a job, one item per queue step, idempotent by item (a material by digest, a note by batch item); a
  cancelled batch removes staged blobs no material references.
- Unmatched subject strings offer «создать предмет» from the existing schedule suggestions (`SuggestSubjects`).

### Old recordings made elsewhere
- An imported audio file becomes a **recording with origin `imported`**: created COMPLETED by
  `recording.NewImported`, `startedAt` = the lecture's date, lesson snapshot from the anchor, the file copied into
  `recordings/<id>.<ext>` through the Storage Manager. Then the existing local STT job (whisper-cli sidecar,
  power-aware) and everything after it run unchanged. **Hard rule 2: the audio never leaves the machine.**
- Formats: those whisper-cli v1.9.5 decodes itself (WAV, FLAC, MP3, OGG; `docs/refs/whisper.md`); duration from the WAV
  header, otherwise unknown until the transcript. Video and M4A/AAC/WMA need a local decoder (Open 4).
- Deleting it follows ADR-0018 and removes only the copy, never the student's original file.

### Handwriting and scans (not decided)
- Until the owner decides, photos (JPEG/PNG, re-encoded by the standard library to drop EXIF/GPS, pixel and size
  limits) and image-only PDFs are pages of a note **without text**: shown, not searched, not cited.
- Recognition is added later behind a port `PageReader` (image → `material.Text` with page locators) run as a job one
  page per step; recognized text is marked «распознано», editable, and shown to the student before it counts as
  their text. Options are listed in Open 3.

### Trust, storage, deletion, limits
- **Untrusted**: note texts, file and folder names, front matter and zip entry names are data. They go to the `ANSWER`
  reader only inside the data block (ADR-0022); the planner never sees them; hints are matched on the desktop.
- **Storage**: all bytes through `BlobStore`/filestore, rows through sqlite (hard rule 5); only the open dialog and
  the drop handler see paths, which never cross a binding back to the page.
- **Dedupe**: the same bytes twice are «уже добавлено» (one material). A changed file is a new material; replacing a
  part keeps the old one removed for Undo.
- **Deletion**: removing a note is soft (Undo); «Удалить навсегда» is a student click in `input/wails` only and
  purges rows, cached text and blobs no other material uses. Answers that cite a removed note show «источник удалён».
  Deleting a recording never deletes notes (anchor → `day`).
- **Limits** (proposed): document 100 MiB (as now), extracted text 4 MiB (as now), pasted text 1 MiB, image 25 MiB and
  40 Mpx, 200 parts per note, batch 500 files / 2 GiB, folder depth 8 / 5000 entries, zip 1 GiB unpacked and 100:1
  ratio, imported audio 2 GiB and 4 h.

## Consequences
- Migrations (next free numbers): `notes`, `note_parts`, `import_batches`, `import_items`, `recordings.origin`.
- `docs/architecture.md` §12 gets `note`, `importbatch`, `notematch`, `pastslots`, `cases/notes`,
  `cases/notesimport`, `adapters/notesource` when this ADR is accepted.
- `answer.Source` and the stored answers change shape (old rows stay valid: section `ownNote` is new).
- Agent tools for notes (attach from the panel) wait for Hub catalog v3; the planner gets only IDs and counters.
- MVP order: typed files and paste attached to a lesson, searchable and citable (TASK-149…156); then bulk import
  (157…160); old recordings (161…164); handwriting (165…169, recognition blocked on Open 3).

## Open (questions to the owner)
1. Naming: the student's notes «Мой конспект» (chip «ваш конспект»), the AI summary stays «Конспект»? *Proposed: yes.*
2. Past lessons inferred from the weekly pattern only for matching, never added to «Расписание» or Google? *Proposed:
   yes.*
3. Handwriting/scans engine: (A) Windows OCR (`Windows.Media.Ocr`, local, free, no CGO via WinRT; good for printed
   scans, weak on handwriting, needs the Russian OCR language); (B) Tesseract CLI sidecar (local, ~50 MB, same
   limits); (C) a local handwriting model (needs an ONNX/Python runtime: not realistic now); (D) images to a vision
   model through a new Hub operation (best on handwriting; the images leave the PC; about 1.5k input tokens per page);
   (E) photos without text only. *Proposed: E now; then D as an explicit opt-in per import with a privacy notice and
   a cost estimate, plus A for printed scans.*
4. Video and M4A/AAC/WMA: Windows Media Foundation through COM syscalls (as WASAPI; built in, Windows N needs the
   Media Feature Pack), a bundled ffmpeg (size, licence), or «convert it yourself»? *Proposed: Media Foundation.*
5. Add a pure-Go PDF text dependency (licence MIT/BSD/Apache, chosen in TASK-156)? *Proposed: yes.*
6. Student's fragments rank before the AI summary in answers, and «Мой конспект» is the first tab of the lesson page?
   *Proposed: yes.*
7. Paid detection with the existing `INSPECT_MATERIAL` reader for files the matcher cannot place? *Proposed: not in
   this backlog; later as «определить по содержимому» on chosen rows with the cost shown.*
8. Obsidian: import is a one-shot copy, the vault is never written; export to Obsidian (former TASK-108) stays a
   separate decision. *Proposed: yes.*
9. Subjects from the schedule: inline «создать предмет» in the import review only; a «завести все предметы из
   расписания» button is a separate task on your word. *Proposed: yes.*
10. The limits above. *Proposed: as listed.*
