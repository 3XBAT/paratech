# ADR-0027: The student's own notes («Мой конспект») for lectures

- **Status:** Accepted for the parts the owner answered on 2026-10-27 (naming, ranking, recognition engine, editing,
  subject removal, Open 11–14); Proposed for the rest (see «Open»)
- **Date:** 2026-10-25, amended 2026-10-27
- **Source:** owner 2026-10-25 (a student starts mid-semester: notes exist handwritten, typed, in files, some
  lectures recorded elsewhere), owner answers 2026-10-27; ADR-0017, ADR-0018, ADR-0019, ADR-0022, ADR-0024, ADR-0025
  (tickets); TASK-118, TASK-122, TASK-133, TASK-138. Tasks TASK-149…169, TASK-287…303, TASK-306…308.

## Context
Today a lecture exists in paratech only as a recording: «База знаний», answers with citations (a cite points at a
summary section, a lecture text block or transcript segments of a recording) and ticket coverage read recordings only.
The library (ADR-0024) stores files by sha256 and links them to schedule lessons, but their text is not searched or
cited. The ICS import starts yesterday (`icsschedule.WindowPast`), so for a student who starts in October the past
lessons are not in the schedule at all. The UI already calls the AI summary «Конспект».

## Decision

### Naming (owner, Open 1: yes)
The student's notes are «Мой конспект», the chip on sources «ваш конспект»; the AI summary stays «Конспект».

### Model (desktop, context `library`)
- **Aggregate `library/note`** (`shared.NoteID`): subject (required), anchor, title (student's text or the file name,
  display only, ≤ 120 characters), ordered parts (1…200), status active/removed, version. A part is a library material
  (ADR-0024: blob by sha256, detected type, soft delete). Pasted text becomes a TXT material (≤ 1 MiB). Provenance is
  always «ваш конспект»; nothing generated is ever stored in a note (recognised text counts only after the student
  accepts it, see Recognition).
- **Anchor** (one of): `recording` (a recorded lecture), `lesson` (a schedule lesson, past or future, without a
  recording), `day` (subject + local date, «пара 12 сентября» with no schedule lesson), `undated` («лекция без даты»).
  Every anchor also stores a snapshot (date, lesson kind, title) so a deleted recording or a re-imported schedule
  falls back to `day` at read time and never orphans or deletes the student's text (no FK from a note to recordings
  or lessons; the ADR-0018 purge is unchanged). Undated lectures stay at the end of a subject (owner, for now; no
  ordering field).
- **Filing**: a part that arrives in «Входящие» is placed in the subject's system folder «Лекции» («Семинары» /
  «Лабораторные» by lesson kind); a material already filed elsewhere stays where it is and is only referenced.
- **Use case `cases/notes`** (add files, paste, write and edit text, replace a part, remove or restore one part,
  reorder, re-anchor, remove/restore the note); materials through a port implemented by `library.Service` (wired in
  `app`, no use case imports another, ARCH-20).

### Editing the text (owner 2026-10-27)
- The student writes and edits note text directly, like a text editor, and removes single parts. A text part is a
  TXT material; files of other types (DOCX, PPTX, PDF, photos) are not editable.
- **Edited in place, blobs by sha256.** A TXT material that is a note part is editable: `material.ReviseText(digest,
  size, expectedRevision, now)` points the same material ID at new bytes (new blob `materials/<sha256>`, written
  before the row). Material ID, note, citations, KB keys and part counts keep working; the text cache is keyed by the
  new digest. Rejected alternative: a new material per save (IDs change on every keystroke pause, every answer cite
  turns into «источник удалён», the library fills with versions).
- `materials` gets `revision` (0 = bytes as imported or pasted) and `origin` (`file` | `typed` | `recognized`) and
  `source_material` (the photo a recognised or typed page text belongs to, else NULL). The global `UNIQUE(sha256)`
  becomes a partial unique index for `revision = 0`: import dedupe («уже добавлено») compares only unedited bytes, an
  edited text may equal another file. One rebuild of `materials` (migration, NO TRANSACTION like 0041).
- **Revisions** (`material_revisions`, part of the material aggregate): saves within 10 minutes of the previous one
  replace the newest revision (autosave does not flood history); after a pause a new one starts; the last 20 are
  kept. A blob that no material and no revision references is deleted with its cached text.
- **Autosave with version check**: the editor saves after 1.5 s without typing and on leaving; the save carries the
  revision it was based on; another revision is `changed` and nothing is overwritten. **Undo**: the editor's own undo
  while typing, and «Вернуть предыдущую версию» steps back through stored revisions (itself a new revision).
- **Remove one part**: `note.RemovePart(id)` / `RestorePart(id, position)` with «Отменить»; the last part cannot be
  removed (the UI offers removing the note). A `typed` or `recognized` material removed from its note is soft-removed
  too (it exists only for the note); a `file` material stays in the library.
- Editing a text from a file changes only paratech's copy, never the original file on disk.
- Limits: 1 MiB per text part (as paste). The editor edits the stored plain text; view mode renders it as Markdown
  (below).

### Showing a note text (owner, Open 11 → б: Markdown)
- Outside the editor a text part is shown as Markdown: headings, paragraphs, line breaks, bold, italic, strikethrough,
  inline code and code blocks, block quotes, ordered and unordered lists (nested), task list marks (shown, not
  clickable), horizontal rules, tables (GFM pipe tables) and links. The text is untrusted (ADR-0022).
- **Renderer: our own, no dependency.** A small parser in the frontend (`src/markdown/`) turns the text into a tree of
  allowed node kinds, and React builds elements from that tree; text goes only into text nodes. No
  `dangerouslySetInnerHTML`, no HTML string, no `eval`, no inline styles from the text. Rejected: `markdown-it` / `marked`
  (they produce an HTML string, which would need `dangerouslySetInnerHTML` plus a sanitizer dependency) and
  `react-markdown` (pulls in the unified/remark tree, dozens of packages, for a subset we need). `package.json` has no
  Markdown package today; none is added.
- **Raw HTML is never interpreted**: an HTML block or inline tag is shown as its literal text. Entities other than
  `&amp; &lt; &gt; &quot; &#39;` stay literal.
- **Links**: only `http:` and `https:` with a host become links; anything else (`javascript:`, `file:`, `data:`,
  relative, `mailto:`) is shown as text. A link is a button-like element showing its text and, in the tooltip, the
  full URL; a click calls a binding, the page never navigates. The binding parses the URL into the library value
  `NoteLink` (http/https, host, no user info, printable ASCII, ≤ 2048 bytes) and opens it in the default browser
  through `weblink` with the defences of a call link (ADR-0032: a temporary `.url` file, no command line, a `%NAME%`
  of an existing variable refused). Autolinks of bare URLs: not made (they stay text).
- **Images are never loaded**: `![alt](url)` is shown as the text «[изображение: alt]»; no `<img>`, no network
  request, no data URL.
- The parser is pure, bounded (nesting depth 16, input ≤ 1 MiB as the part limit) and fuzzed; on any internal limit it
  falls back to plain text. Search sections still come from the `#` lines of the stored text (unchanged).
- Edits are the student's text: provenance stays «ваш конспект» (and «распознано» for recognised text).

### Text
- `material.Text` gets `LocatorSection` (a heading of Markdown or a Heading style of DOCX; the heading is the first
  line of the fragment). Markdown is still detected as TXT by content; a light reader in the domain drops YAML front
  matter (its `date`/`subject`/`tags` become import hints only), turns `[[x|y]]` into `y`, drops embeds and HTML. Notes
  are never rendered as HTML.
- PDF text (postponed by ADR-0024) gets a pure-Go reader in `adapters/doctext` (dependency chosen and recorded by
  TASK-156); a PDF without a text layer is a scan (see Recognition).
- The extracted text of a note part is cached as a derived artifact by its digest (rebuildable, removed with the blob),
  so a question does not unzip every DOCX again (ADR-0019 stays: no index).

### Knowledge base, answers, tickets
- A KB lesson is a recording or a note-only lecture (`lesson`/`day`/`undated` anchor); notes with a `recording`
  anchor, or a `lesson` anchor whose lesson has a recording, join that recording's lesson.
- Search and `knowledgebase.Passages` take note fragments; `answer.Source` gets section `ownNote` with note, material
  and locator (page / slide / section / fragment) and an empty recording. The card shows «ваш конспект · стр. 3» or
  «ваш конспект · раздел „Ряды“».
- **Ranking (owner, Open 6: yes)**: on an equal match the student's fragment ranks before the AI summary, and inside a
  lesson note fragments are scanned first (so the hit cap never cuts them first). «Мой конспект» is the first tab of
  the lecture page.
- Tickets (ADR-0025) use `answering.Ask`, so notes count in coverage with no change of the rule; «нет в ваших
  лекциях» reads «нет в ваших лекциях и конспектах».
- **Coexistence**: the AI summary and lecture text of a recorded lecture are never merged into or written over the
  student's text, and the reverse.

### Removing a subject that has notes (owner, К9 (б))
- The student may remove a subject that has notes: the notes go with it, after a warning («Вместе с предметом
  удалятся N конспектов») and with «Вернуть». This replaces the `ErrHasNotes` refusal of TASK-151 for the student's
  click only.
- **Soft removal of the subject**: `subject.Remove(now)` / `Restore(now)` (status active/removed, `subjects.removed_at`);
  one aggregate in one transaction (ARCH-23). Notes, parts and materials are not rewritten: every reader hides the
  notes and materials of a removed subject (library lists, KB, answers, tickets, search, agent tools, mention
  candidates, subject matching). «Вернуть» restores the subject and everything shows again as it was.
- A removed subject keeps its name: creating the same name answers «Предмет с таким названием удалён» with «Вернуть».
- New method `DiscardSubject(ctx, id, expectedVersion)` used only by `input/wails` on the student's click. The old
  `RemoveSubject` (the agent's Undo of `create_subject`) keeps refusing a subject with notes: an agent Undo never
  takes the student's notes with it.
- A subject without notes and files is removed as before.
- **Ordinary files go with the subject** (owner, Open 12 → а): files of the subject that are not note parts, in any of
  its folders and nested folders, are hidden with it (soft, no row rewritten) and come back with «Вернуть». The warning
  names both counts («Вместе с предметом удалятся N конспектов и M файлов»). A material's visibility follows the
  subject of its folder; a material filed under the removed subject that is a part of an active note of another
  subject stays visible in that note.
- **Purge after 3 days** (owner, Open 13: «стирается совсем через 3 дня»): `subject.RestoreWindow` = 3 days; `Restore`
  after it → `ErrRestoreExpired` (the binding answers `expired`). A new use case `cases/subjectpurge` (its own narrow
  ports over the library repositories and the blob store, no import of another use case, ARCH-20; startup cleanup and
  once a day, the pattern of the assignment purge of ADR-0029) removes for each subject removed longer than the
  window, each step its own transaction and idempotent, the subject row last: its notes (rows, parts, revisions,
  cached text, blobs no other material or revision uses — the same repository methods a later «Удалить навсегда»
  reuses), its other materials the same way, its folders, aliases and teachers. A material still a part of an active note of another subject is kept
  and moved to «Входящие». Assignments, recordings and schedule lessons are other contexts and stay; their subject
  name falls back as for an unknown subject (ADR-0024). After the purge the name is free.

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
  date, else a slot inferred by `schedule/services/pastslots` (Open 2). Each proposal carries a confidence (sure /
  likely / none) and reason enums; nothing is sent to a model.
- **Commit** is a job, one item per queue step, idempotent by item (a material by digest, a note by batch item); a
  cancelled batch removes staged blobs no material references.
- Unmatched subject strings offer «создать предмет» from the existing schedule suggestions (`SuggestSubjects`).

### Old recordings made elsewhere
- An imported audio file becomes a **recording with origin `imported`**: created COMPLETED by
  `recording.NewImported`, `startedAt` = the lecture's date, lesson snapshot from the anchor, the file copied into
  `recordings/<id>.<ext>` through the Storage Manager. Then the existing local STT job (whisper-cli sidecar,
  power-aware) and everything after it run unchanged. **Hard rule 2: the audio never leaves the machine.**
- Formats: those whisper-cli v1.9.5 decodes itself (WAV, FLAC, MP3, OGG; `docs/refs/whisper.md`).
- **Amendment (TASK-162):** the duration must be known from the headers (or counted MP3 frames) and be at most 4 h;
  a file whose headers give no duration (WAV whose data chunk is not found, FLAC with total 0, chained Ogg,
  free-format MP3, MP3 without two consecutive frames) is refused with «Не удалось узнать длительность записи в этом
  <формат>-файле» (TASK-301: the refusal names the file format). A WAV data chunk past the first 64 KiB is looked for
  by a bounded scan of chunk headers (at most 64 reads of 8 bytes, never a chunk body).
- Video and M4A/AAC/WMA: deferred by the owner (Open 4); TASK-164 stays blocked.
- Deleting it follows ADR-0018 and removes only the copy, never the student's original file.

### Recognition of photos and scans (owner, Open 3)
- Photos (JPEG/PNG, re-encoded by the standard library to drop EXIF/GPS, pixel and size limits) and image-only PDFs
  are pages of a note; until recognised they are shown, not searched, not cited (TASK-165/166).
- **Setting «ИИ распознаёт фото конспектов»** (`settings`, typed column): `off` | `local` (Windows OCR); a `cloud`
  value (vision model through the Hub) is added later. The default is `off` (owner, Open 14 → а): the student turns
  it on. With `local` a new photo page is
  queued for recognition; older pages get «Распознать».
- **Engine now: Windows OCR** (`Windows.Media.Ocr`): local, free, no network, no CGO — WinRT reached through syscalls
  of `golang.org/x/sys/windows` in `adapters/winocr` (`//go:build windows`, stub elsewhere). TASK-293 proves it on the
  dev machine first and stops if it is not feasible. It is good on printed scans and weak on handwriting.
- **Russian OCR language**: the engine is created for `ru` only; when Windows has no Russian recognizer the page is
  `no_language` and the UI says how to add it («Параметры → Время и язык → Язык → Русский → Распознавание текста»);
  paratech never installs it itself (needs admin) and never falls back to another language. «Повторить» after
  installing.
- Behind the port `PageReader` (image → lines with the engine version), run as a job one page per step, idempotent
  by page digest and engine version, power-aware like STT. The result is stored as a pending recognition, marked
  «распознано», editable, and shown next to the photo before it counts; on «Принять» (as is or edited) it becomes a
  TXT material with `origin = recognized` and `source_material` = the photo, placed right after the photo.
  Unaccepted text is never searched, cited or sent anywhere.
- **Later**: a vision model through a new Hub operation (TASK-168), opt-in per import with a privacy notice and a cost
  estimate; images then leave the PC, so it is a separate setting value and a separate decision.

### Trust, storage, deletion, limits
- **Untrusted**: note texts (typed, recognised, from files), file and folder names, front matter and zip entry names
  are data. They go to the `ANSWER` reader only inside the data block (ADR-0022); the planner never sees them; hints
  are matched on the desktop.
- **Storage**: all bytes through `BlobStore`/filestore, rows through sqlite (hard rule 5); only the open dialog and
  the drop handler see paths, which never cross a binding back to the page.
- **Dedupe**: the same bytes twice are «уже добавлено» (one material, unedited bytes only). A changed file is a new
  material; replacing a part keeps the old one removed for Undo.
- **Deletion**: removing a note is soft (Undo); «Удалить навсегда» is a student click in `input/wails` only and
  purges rows, revisions, cached text and blobs no other material uses. Answers that cite a removed note show
  «источник удалён». Deleting a recording never deletes notes (anchor → `day`).
- **Limits** (proposed, Open 10): document 100 MiB (as now), extracted text 4 MiB (as now), text part 1 MiB, image
  25 MiB and 40 Mpx, 200 parts per note, batch 500 files / 2 GiB, folder depth 8 / 5000 entries, zip 1 GiB unpacked and
  100:1 ratio, imported audio 2 GiB and 4 h.

## Consequences
- Migrations (next free numbers, one at a time): `notes`, `note_parts` (0036), `recordings.origin` (0042),
  `import_batches`, `import_items`; `materials` rebuild with `revision`/`origin`/`source_material` and
  `material_revisions` (TASK-290); `subjects.removed_at` (TASK-288); `settings.note_recognition` default `off`
  (TASK-294);
  `page_recognitions` (TASK-167).
- `docs/architecture.md` §12 names `note`, the editable text of `material`, the subject removal; `importbatch`,
  `notematch`, `pastslots`, `cases/notesimport`, `cases/noterecognition`, `adapters/notesource`, `adapters/winocr` are
  added when their tasks land.
- `answer.Source` and the stored answers change shape (old rows stay valid: section `ownNote` is new). A cite of an
  edited text opens the current text; the stored quote stays as it was answered.
- Agent tools for notes (attach from the panel) wait for Hub catalog v3; the planner gets only IDs and counters.
- MVP order: typed files and paste attached to a lesson, searchable and citable (TASK-149…156); editing and part
  removal (290…292); subject removal (288, 289); recognition (293, 294, 167, 169); then bulk import (157…160); old
  recordings (161…163).

## Open (questions to the owner)
2. Студент начал учиться в середине семестра, а прошедших пар в расписании нет: календарь отдаёт пары только с
   сегодняшнего дня. Можно ли достраивать прошедшие пары по недельному шаблону (матан по вторникам в 10:00 — значит,
   и 12, 19, 26 сентября) только чтобы подсказать, к какой лекции отнести файл, — без записи этих пар в «Расписание»
   и в Google Календарь?
4. Видео и M4A/AAC/WMA — отложено владельцем 2026-10-27.
5. Add a pure-Go PDF text dependency (licence MIT/BSD/Apache, chosen in TASK-156)? *Proposed: yes.*
7. Paid detection with the existing `INSPECT_MATERIAL` reader for files the matcher cannot place? *Proposed: not in
   this backlog; later as «определить по содержимому» on chosen rows with the cost shown.*
8. Конспекты из Obsidian paratech только копирует к себе один раз: ваша папка Obsidian не меняется, а правки в
   paratech не попадают обратно в Obsidian и наоборот. Выгрузка конспектов из paratech в Obsidian — отдельное решение
   позже; так подходит?
9. Subjects from the schedule: inline «создать предмет» in the import review only; a «завести все предметы из
   расписания» button is a separate task on your word. *Proposed: yes.*
10. The limits above. *Proposed: as listed.*

Answered 2026-10-27: 11 — (б) Markdown without raw HTML; links only http/https, opened by a click; images not loaded
(«Showing a note text»); 12 — (а) ordinary files, nested folders included, go with the subject, soft, with «Вернуть»;
13 — a removed subject is purged completely after 3 days; 14 — (а) photo recognition is off by default.
