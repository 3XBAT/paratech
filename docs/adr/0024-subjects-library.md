# ADR-0024: Subjects and library

- **Status:** Accepted
- **Date:** 2026-10-22
- **Source:** TASK-112; design `agent-core-design-2026-10.md` §2.3, §2.6, §5 (outside the repo); ADR-0019, ADR-0021.

## Context
The agent creates subjects and folders, places uploaded files and links them to lessons. ADR-0019 groups lessons by a
free subject string (case-insensitive). Threads (ADR-0021), «Предметы» navigation and Undo need a stable subject ID.

## Decision
- **New context `library`** (desktop):
  - `subject`: name, aliases, semester, system folders (Лекции, Семинары, Лабораторные, Материалы, ДЗ, Билеты; not
    removable) and own folders as entities of the aggregate. A folder name is a value object (length,
    Windows-forbidden characters, CON/NUL).
  - `material`: sha256, size, type, original name (display only, never sent to the planner), subject, folder, lesson
    link, status active/removed, text status.
  - `values/semester`; `services/subjectmatch`: schedule string → subject ID (case, «ё» = «е», aliases).
- **Virtual library**: folders live in SQLite; files sit in the private artifact store by sha256 (dedup). New
  uploads land in «Входящие». No visible folder on disk: Undo stays safe, no sync/OneDrive issues. «Открыть в Word»
  opens a working copy written as `<material-id>.<ext>` in a private directory; `ext` comes from the detected type
  allowlist (docx, pptx, txt), never from the original file name, so `.lnk`/`.hta` never reach ShellExecute.
- **Replaces the free string of ADR-0019**: «База знаний» groups by subject ID. Subjects are suggested from the
  schedule, never created automatically.
- **Use case `cases/library`**: Subjects, CreateSubject, CreateFolder, RemoveSubject/RemoveFolder (empty only), Import,
  Place, Link/Unlink, RemoveMaterial/RestoreMaterial (soft delete, not an agent tool), Inspect. Agent tools
  `list_subjects`, `create_subject`, `create_folder`, `place_upload`, `link_material`, `inspect_upload` call it
  (ADR-0021).
- **Material outputs**: summary and homework search of a material run as jobs like those of a transcript;
  `summaries`/`assignments` get a link to the material (own migration).
- **Text extraction**: DOCX, PPTX (`archive/zip`) and TXT without dependencies; PDF text is postponed: the material
  shows «текст PDF не извлечён».

## Consequences
- Desktop migrations (next free numbers): `subjects`, `subject_aliases`, `subject_folders`, `materials`,
  `material_lessons`; then the material outputs.
- Undo of library actions: an empty subject or folder is removed, a placed file goes back to «Входящие», a link is
  removed; the file itself is never deleted by Undo.
- A visible `%USERPROFILE%\paratech` folder, if ever wanted, needs its own ADR.
- `library.Service` implements the knowledgebase subject-resolver port (wired in `app`).
- Settled (TASK-121): no backfill. A free subject string resolves to a subject ID at read time via `subjectmatch`
  (name or alias); an unresolved string keeps the legacy grouping of ADR-0019.
