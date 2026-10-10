# Learning Assistant (paratech)

Local-first Windows assistant for **online** classes: records system audio, transcribes locally (whisper.cpp), summarizes and extracts homework
via a cloud LLM Hub, syncs deadlines to Google Calendar. Requirements: `docs/prd/learning-assistant-prd-v0.4.md`.

## Repository layout
| Path | What |
|---|---|
| `paratech/` | **The code** (`desktop/`, `hub/`, `shared/`, `go.work`). Kept self-contained so it can be split into its own repo later |
| `docs/` | PRD, **architecture rules (`docs/architecture.md`)**, ADRs, task drafts, process prompts, reference notes |
| `CLAUDE.md`, `.claude/` | Rules and subagents (`architect`, `go-core`, `hub-dev`, `frontend`, `tester`, `reviewer`) for Claude Code |
| `tasks.json`, `progress.txt`, `ralph.sh` | Autonomous "ralph" loop: task list, memory log, runner |

## Setup on Windows
1. Install **Git for Windows** (provides Git Bash, needed for `ralph.sh` and hooks), **Go 1.26+**, **Node.js LTS**, and **Claude Code**.
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

## Autonomous mode (ralph loop)
```bash
RALPH_MAX_ITERATIONS=3 ./ralph.sh      # first run: a few iterations only
./ralph.sh                             # then the full loop
```
- Each iteration: a fresh `claude` session picks ONE pending task from `tasks.json`, implements it in `paratech/` following `docs/architecture.md`,
  runs the checks, sets the task status, appends to `progress.txt` and commits. The script creates a `ralph/<timestamp>` branch and never pushes.
- Tasks that need a human (checks on real Windows, decisions) end up `blocked`; read `progress.txt` for the reason, do the check, then set `done` by hand.
- Options: `RALPH_AGENT=claude|codex`, `RALPH_MAX_ITERATIONS`, `RALPH_MAX_STALLS`, `RALPH_REVIEW=0|1`.
- Review the branch (`git log`, `git diff master...`) before merging into `master`.

## Setup on macOS / Linux
`brew install go node git`, then the same steps in a normal shell. Windows-only code is behind build tags: check it with
`(cd paratech && GOOS=windows go vet ./desktop/...)`.
