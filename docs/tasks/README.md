# Tasks

> The autonomous ralph loop executes `tasks.json` in the repository root (generated via `docs/process/prd-to-tasks.prompt.md`). The files in this folder are human-written specs and drafts that feed that generation.

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

## Next tasks to be written by `architect` after T-002
- T-003 SQLite schema, migrations and Storage Manager
- T-004 Persistent Job Store and worker pool (with power-aware states)
