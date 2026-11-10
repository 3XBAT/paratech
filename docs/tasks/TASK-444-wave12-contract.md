# TASK-444: wave 12 on the desktop — tool specs and actionpolicy rules (not served yet)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-443
- **PRD sections / AC:** ADR-0038 §2, §3, §5, §6; ADR-0033 §5–§7; ADR-0021 (classes)
- **Агент:** declares catalog 12 tools; dispatch in TASK-445…450, 452; served by TASK-456
- **Branch:** ralph/<date>-task-444

## Goal
Declare every wave-12 tool and rule in `domain/agent/values` (file `wave12.go`, served only for catalog ≥ 12) and
`actionpolicy`, so the dispatch tasks run in parallel on disjoint files.

## Specs (contract; argument types as in catalog 10/11)
| Tool | Class | Arguments | Notes |
|---|---|---|---|
| `tickets` | U; `confirm` U paid | `action` propose_text\|propose_material\|confirm\|cancel\|restore, `text` (ticket_text ≤ 16 KiB, the student's pasted list), `material` id, `list` id, `subject` id | each action only its own arguments; `confirm` needs a plan when above the paid threshold |
| `list_tickets` | R | `subject` id (optional) | list IDs, state, ticket count, parts |
| `work` | U / R paid | `action` stop\|regenerate\|retry\|ask, `work` id, `question` query (ask only) | stop: U without Undo (line offers «Продолжить»); regenerate: paid, always a plan (replaces a ready result); retry: paid; ask: R paid, the answer goes to a card |
| `job` | U | `action` run_now\|cancel_transcription\|retranscribe, `recording` id | run_now: no Undo; cancel: Undo = retranscribe |
| `list_sources` | R | none | source IDs, `kind` system\|app, `app` enum (zoom, teams, telemost, meet_browser, browser, other); no window titles |
| `recording` (changed) | U | + `switch_source` (`source` id), `skip_prompt` | switch: Undo = previous source |
| `list_chats` | R | `state` open\|archived | chat IDs, created/updated dates, request counts; no titles |
| `chat` | U | `action` archive\|restore\|rename, `chat` id, `title` label (rename) | never the current chat for archive; rename only when the student asks explicitly (ADR-0038 О-4) |
| `list_actions` | R | `from`, `to` dates, `chat` id (optional) | action IDs, tool, status, date, target IDs |
| `undo` | U without Undo | `action` id | the journal's own compensation; refusals as «Отменить» |
| `list_models` | R | none | model IDs, size class, installed, default |
| `model` | U / C | `action` set_default\|delete, `model` id | delete: C card |
| `propose_schedule_import` | C | `upload` id (ICS) | the page's preview card; «Да» = Commit |
| `set_mail_recipient` | C | `email` | value card, ADR-0030 rules |
| `get_profile` | R | none | only if ADR-0038 О-6 = а; else not declared |
| `list_subjects` (changed result) | R | — | teacher e-mails only if О-6 = а |
| `note` (changed) | U | + `replace_file` (`part` id, `upload` id), `reorder` (`note` id, `parts` ids) | `list_notes{note}` gives part IDs |
| `change_assignment` (changed) | U | + `teacher` label | the extracted teacher text, a label |
| `do_assignment` | U paid | `assignment` id | only if ADR-0038 О-5 = а; the route of «Сделать задание» (TASK-411) |

New ID kinds as needed: `ticket_list`, `chat`, `action`, `source`, `model`, `part`, `delivery` (if not there).

## Allowed to change
- `paratech/desktop/internal/domain/agent/values/*` (wave12.go and the catalog list), `domain/agent/services/actionpolicy/*`,
  `domain/agent/action/*` (only new compensation kinds), `domain/shared/*` (only new ID kinds) + tests

## Acceptance criteria
- [ ] Specs parse (table tests per action: own arguments only, refusals); catalog ≤ 11 does not see them.
- [ ] Policy table: classes above; `undo` and `work{stop}` / `job{run_now}` have no compensation; `model{delete}`,
      `propose_schedule_import`, `set_mail_recipient` never run in `input/agent` (no Confirm/Execute method test).
- [ ] Estimated prefix delta recorded in progress (target ≤ +2 500 tokens over v11).
- [ ] `go vet`, `go test -race` for domain/agent and the architecture test pass.
