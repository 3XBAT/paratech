# Tasks

> These drafts predate `docs/architecture.md`: where a path or structure here differs from the architecture or from `tasks.json`, the architecture and `tasks.json` win.
>
> The task loop executes `tasks.json` in the repository root. The files in this folder are early human-written specs and drafts; they are history, not the current queue.

Status legend: `todo` / `doing` / `review` / `done`. Each task lives in its own file (copy `_TEMPLATE.md`).
Phases correspond to PRD §17. Items without a file yet are epics: ask the `architect` agent to break them down before implementation.

## Foundation and spikes (do first)
| ID | Title | Agent | Depends | Status |
|---|---|---|---|---|
| T-001 | Repo tooling: lint config, buf setup, CI sanity | go-core | - | todo |
| T-002 | Contracts v0: Hub proto skeleton, SQLite schema draft, core Go interfaces | architect | - | todo |
| S-001 | Spike: Wails v3 non-focus-stealing popup, tray, notification (Windows) | go-core | T-001 | todo |
| S-002 | Spike: system audio (WASAPI loopback) capture options from Go (Windows) | go-core | T-001 | todo |
| S-003 | Spike: whisper-cli sidecar run on a sample recording (Windows) | go-core | T-001 | todo |
| S-004 | Research: Google OAuth verification vs test mode, `calendar.app.created` behavior | architect | - | todo |

## Epics (by PRD phase)
| Phase | Epic | Notes |
|---|---|---|
| 1 | Windows foundation: Storage Manager, SQLite, settings, Scheduler | first vertical slice: T-003 / T-004 |
| 2 | Recording UX: popup, Recording Manager, watchdog, crash recovery | depends on S-001, S-002 |
| 3 | Persistent jobs: store, workers, retry, recovery, `WAITING_FOR_POWER` | PRD §7 |
| 4 | Local STT and Model Manager (download/import, verify, pin) | depends on S-003 |
| 5 | LLM Hub: gateway, API keys, quota, provider adapter | PRD §4.2, §9 |
| 6 | AI post-processing: summary, homework/deadline extraction | |
| 7 | Homework assistant and confirmed email delivery (SMTP) | |
| 8 | Schedule import (ICS/CSV/XLSX) and Google Calendar adapter | depends on S-004 |
| 9 | Security and reliability hardening | |
| 10 | Closed pilot | |

## Agent panel: waiting, «Стоп», repeat, streaming (ADR-0026, 2026-10-24)
| ID | Title | Agent | Depends |
|---|---|---|---|
| [TASK-139](TASK-139-agent-waiting-indicator.md) | Waiting indicator | frontend | - |
| [TASK-140](TASK-140-agent-stop-in-flight.md) | «Стоп» during a running step (core) | go-core | - |
| [TASK-141](TASK-141-agent-rerun.md) | «Повторить» a finished request (core) | go-core | 140 |
| [TASK-142](TASK-142-answer-key-per-request.md) | Answer key scoped to the request | go-core | - |
| [TASK-143](TASK-143-agent-stop-rerun-ui.md) | «Стоп» and «Повторить» (UI) | frontend | 139, 140, 141 |
| [TASK-144](TASK-144-anthropic-streaming-probe.md) | Anthropic streaming + gateway probe | hub-dev | - |
| [TASK-145](TASK-145-hub-completestream.md) | Hub `CompleteStream` for ANSWER | hub-dev | 144 |
| [TASK-146](TASK-146-desktop-answer-stream.md) | Desktop streamed ANSWER and draft | go-core | 142 |
| [TASK-147](TASK-147-agent-draft-event.md) | Draft to the panel (Driver, Wails event) | go-core | 141, 146 |
| [TASK-148](TASK-148-agent-draft-render.md) | Progressive answer text (UI) | frontend | 143, 147 |

## The student's own notes «Мой конспект» (ADR-0027, 2026-10-25)
MVP (medium): typed files and paste on a lecture, searchable and citable. Then bulk import, old recordings,
photos; recognition is blocked on the owner's decision (ADR-0027 Open 3), video on Open 4.

| ID | Title | Agent | Depends |
|---|---|---|---|
| [TASK-149](TASK-149-note-aggregate.md) | Note aggregate and storage | go-core | - |
| [TASK-150](TASK-150-note-text-sections.md) | Text of notes: Markdown, DOCX headings, cache | go-core | - |
| [TASK-151](TASK-151-notes-usecase.md) | `cases/notes` and NotesService binding | go-core | 149 |
| [TASK-152](TASK-152-notes-in-knowledge-base.md) | Notes in «База знаний» and search | go-core | 149, 150 |
| [TASK-153](TASK-153-notes-citations-coverage.md) | Citations of notes, ticket coverage | go-core | 152 |
| [TASK-154](TASK-154-lesson-own-notes-ui.md) | «Мой конспект» on the lesson page | frontend | 151 |
| [TASK-155](TASK-155-kb-own-notes-ui.md) | Note-only lectures, past lesson, citation labels | frontend | 152, 153, 154 |
| [TASK-156](TASK-156-pdf-text.md) | PDF text (pure Go) | go-core | 150 |
| [TASK-157](TASK-157-note-matching.md) | Matching to past lessons (domain) | go-core | 149 |
| [TASK-158](TASK-158-notesource-adapter.md) | Folder and zip source | go-core | - |
| [TASK-159](TASK-159-notes-import-batch.md) | Import batch: stage, propose, commit | go-core | 151, 157, 158 |
| [TASK-160](TASK-160-notes-import-review-ui.md) | Import review screen | frontend | 159, 155 |
| [TASK-161](TASK-161-import-old-recording.md) | Import an old audio recording | go-core | - |
| [TASK-162](TASK-162-import-recording-ui.md) | «Импортировать запись лекции» | frontend | 161 |
| [TASK-163](TASK-163-batch-audio-items.md) | Audio in a bulk import | go-core | 159, 161 |
| [TASK-164](TASK-164-video-audio-decode.md) | Video/M4A decode (blocked: Open 4) | go-core | 161 |
| [TASK-165](TASK-165-photo-pages.md) | Photos as pages without text | go-core | 151 |
| [TASK-166](TASK-166-photo-pages-ui.md) | Photo pages (UI) | frontend | 154, 165 |
| [TASK-167](TASK-167-page-recognition-core.md) | Page recognition (blocked: Open 3) | go-core | 165 |
| [TASK-168](TASK-168-hub-read-handwriting.md) | Hub handwriting reader (blocked: Open 3 = D) | hub-dev | - |
| [TASK-169](TASK-169-recognized-text-review-ui.md) | Check recognized pages (blocked) | frontend | 166, 167 |

## UX quick fixes, navigation and permissions (ADR-0034, 2026-11-03)
| ID | Title | Agent | Depends |
|---|---|---|---|
| [TASK-344](TASK-344-ui-words.md) | One word per thing (glossary pass) | frontend | - |
| [TASK-345](TASK-345-today-quick-fixes.md) | «Сегодня» quieter and clearer | frontend | 344 |
| [TASK-346](TASK-346-homework-journal-quick-fixes.md) | «Задания» and «Журнал» quick fixes | frontend | 344 |
| [TASK-347](TASK-347-recordings-subjects-quick-fixes.md) | «Записи», «Предметы», dialogs | frontend | 344 |
| [TASK-348](TASK-348-rail-labels-recordings.md) | Rail labels, «Записи» section, «Записать пару» | frontend | 345, 347 |
| [TASK-349](TASK-349-busy-pill.md) | Busy pill in the rail | frontend | 348 |
| [TASK-350](TASK-350-breadcrumbs.md) | Breadcrumbs | frontend | 346, 347, 348 |
| [TASK-351](TASK-351-lesson-route.md) | One lesson screen: route and page | frontend | 350 |
| [TASK-352](TASK-352-lesson-entry-points.md) | All entry points to the lesson screen | frontend | 351 |
| [TASK-353](TASK-353-subject-tabs.md) | Subject tabs and «Билеты к экзамену» | frontend | 352 |
| [TASK-354](TASK-354-fold-knowledge-base.md) | «База знаний» folded into «Предметы» (В1) | frontend | 353 |
| [TASK-355](TASK-355-agent-panel-context.md) | Agent panel context chip and suggestions | frontend | 345, 352 |
| [TASK-356](TASK-356-settings-structure.md) | Settings menu structure | frontend | 348 |
| [TASK-357](TASK-357-first-run-checklist.md) | «Начало работы» checklist | frontend | 348, 355 |
| [TASK-358](TASK-358-permissions-domain.md) | Permissions: domain and storage | go-core | - |
| [TASK-359](TASK-359-permissions-usecases.md) | Permissions: use cases | go-core | 358 |
| [TASK-360](TASK-360-permissions-binding.md) | Permissions: bindings and trigger | go-core | 359 |
| [TASK-361](TASK-361-permissions-ui.md) | «Разрешения» and «Больше не спрашивать» (UI) | frontend | 356, 360 |
| [TASK-362](TASK-362-do-assignment-entry.md) | One entry «Сделать задание» (В2) | frontend | 346 |

## Next tasks to be written by `architect` after T-002
- T-003 SQLite schema, migrations and Storage Manager
- T-004 Persistent Job Store and worker pool (with power-aware states)
