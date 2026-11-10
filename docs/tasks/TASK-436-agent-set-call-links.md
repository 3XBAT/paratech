# TASK-436: agent tool `set_call_links` — dispatch, confirm card, reading links per kind

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-432, TASK-434, TASK-435
- **PRD sections / AC:** ADR-0038 §1, §4, §5; ADR-0033 §8; ADR-0022; ADR-0021 (journal, provenance)
- **Агент:** `set_call_links` (C, batch card `call_links`, wave 11); `list_subjects.call_links`; `offer_button`
  `open_call_link` / `copy_call_link` (UI) — served with catalog 11 (TASK-443)
- **Branch:** ralph/<date>-task-436

## Goal
«Внеси все эти ссылки» with a pasted timetable becomes one card that lists every row with what will be saved, and one
«Да» saves the ticked ones.

## Tool (spec in TASK-434; catalog 11)
```json
{"name": "set_call_links", "class": "C", "paid": false, "agent_can_execute": false, "arguments": [
  {"name": "rows", "type": "call_link_rows", "required": true, "max_items": 40}
]}
```
`call_link_rows`: list of `{subject: id | subject_name: label, kind: enum general|lecture|seminar|practice|lab,
link: call_link (text ≤ 2048 bytes, optional), status: enum link|in_person|unconfirmed|none|clear}`; arguments ≤ 16 KiB.

## Flow
1. `input/agent/calllinks.go`: every `subject` ID passes the typed provenance check (one unknown → `is_error
   unknown_id`, nothing proposed); rows go to `cases/library.ProposeCallLinks`; the card `call_links` keeps the token
   and the items.
2. Result to the planner: `{"card": true, "rows": n, "ready": k, "skipped": {"in_person": 1, …}}` — counters and reason
   codes only; no link, no name.
3. `input/wails/agentconfirm_calllinks.go`: `ConfirmCallLinks(cardID, choices)` → `ApplyCallLinks`; the outcome is
   stored in the card for «Отменить всё» (`UndoCallLinks`); the journal entry becomes DONE with that compensation;
   `STALE` → the card says «Ссылки изменились» with «Показать снова» (a new proposal from the same rows).
   «Нет» → declined, nothing changes.
4. `list_subjects` result for catalog ≥ 11: `call_links{general, lecture, seminar, lab}` (only the set ones, canonical,
   same bounds as today's `call_link`); catalog ≤ 10 keeps `call_link` = the general link.
5. `offer_button{open_call_link | copy_call_link}`: with `lesson` → its subject (subjectmatch) and kind → `Join`; with
   `subject` (+ optional `kind`) → that subject; no link → result `no_link` (the planner answers as for «дай ссылку»);
   the card button calls the same bindings as «Подключиться» (TASK-432). Remove `offerUnavailable`.

## Allowed to change
- `paratech/desktop/internal/input/agent/calllinks*.go` (new), `offer*.go`, `tools.go` (only the list_subjects mapping
  hook, if it lives there) + tests
- `paratech/desktop/internal/domain/agent/values/read.go` (only `call_links`) + test
- `paratech/desktop/internal/input/wails/agentconfirm_calllinks.go` (new), `agentconfirm_offer.go` + tests
- `paratech/desktop/internal/app/*` (only wiring)

## Acceptance criteria
- [ ] Owner-shaped fixture (15 rows) → one card, correct outcomes, planner result has counters only (exact JSON test).
- [ ] Unknown ID refuses the call; a row with a link from a tool result is impossible (the tool takes no IDs of
      materials; eval case in TASK-442).
- [ ] Confirm: all ticked, a subset, stale, repeat, «Отменить всё», decline; journal states.
- [ ] No link, name or label in any log line or planner result (log handler test).
- [ ] `go vet`, `go test -race` for input/agent, input/wails, domain/agent, app and the architecture test pass;
      `GOOS=windows go vet`.
