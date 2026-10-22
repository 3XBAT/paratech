# paratech

paratech (formerly «Learning Assistant», ADR-0015) is a local-first Windows assistant for **online** classes: records system audio, transcribes locally (whisper.cpp), summarizes and extracts homework
via a cloud LLM Hub, syncs deadlines to Google Calendar. Requirements: `docs/prd/learning-assistant-prd-v0.4.md`.

## Repository layout
| Path | What |
|---|---|
| `paratech/` | **The code** (`desktop/`, `hub/`, `shared/`, `go.work`). Kept self-contained so it can be split into its own repo later |
| `docs/` | PRD, **architecture rules (`docs/architecture.md`)**, ADRs, task drafts, process prompts, reference notes |
| `CLAUDE.md`, `.claude/` | Rules and subagents for Claude Code |
| `tasks.json`, `progress/` | Task list and one report per task (`progress.txt` is the frozen log before 2026-10-21) |

## Setup on Windows
1. Install **Git for Windows** (provides Git Bash, needed for hooks and scripts), **Go 1.26+**, **Node.js LTS**, and **Claude Code**.
2. Open **Git Bash** and clone:
   ```bash
   git clone https://github.com/3XBAT/paratech.git
   cd paratech
   ```
   (The repository root is the working directory for Claude Code; the code is in the `paratech/` subfolder.)
3. Check the toolchain:
   ```bash
   (cd paratech && go build ./desktop/... ./hub/... ./shared/... && go vet ./desktop/... ./hub/... ./shared/...)
   ```
4. Start Claude Code in the repository root and work interactively:
   ```bash
   claude
   ```
   Project subagents in `.claude/agents/` are loaded automatically. Read `CLAUDE.md` and `docs/architecture.md` first.

## Task loop
The main Claude Code session acts as supervisor: it runs one `coordinator` agent per task (one branch, one PR), audits the result and merges on green CI.
See `CLAUDE.md` («Task loop») and `docs/process/supervision.md`.

The product targets Windows only; CI on Linux builds the stubs behind `//go:build windows`.
