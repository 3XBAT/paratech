# TASK-128: confirmation card for irreversible actions (frontend)

Done: TS wrapper bindings/agentConfirm.ts (Card, Confirm, Decline, ProposeDeadline; strict parsing, allowlist of core error texts). Card in the panel (agent/ConfirmCard.tsx, agentConfirmView.ts): send shows recipient from settings, subject, assignment, attachment (name, format, size), version, mentions_email notice; calendar shows target, title, subject, due; deletion shows what goes and opens the existing RecordingDeleteDialog (Decline only changes the card). «Да» is a click only: no autofocus, no key handlers, «Нет» first, double click = one call. All values are text nodes. Red only on the card. «Да, в календарь» in the Today posts section: ProposeDeadline, then the card with its own «Да». No dependency, no Go change.

Review: approve; M1 (refused Да/Нет left a stale card with active Да) fixed with tests (card re-read); lows fixed (deadline filter order, card key by target, neutral outcome text, ErrSendFormat text). Re-review not repeated (small fix). Security auditor: no findings.

## Decisions to confirm
- Today needs two clicks: «Да, в календарь» proposes, the card with exact title/date asks «Да».
- Button sits in the Posts section, not the lesson chip.

## Follow-ups
- Core: Today post lists a calendar action for every assignment with a due date even when its deadline proposal already ran; the button reappears after remount (no duplicate event, calendar upserts).
- «Срок» row shows weekday and date only (no time/year).
- Today does not refresh on agent:update (as in TASK-126).
- A Go test could pin the Wails method names used by the TS wrapper.

## Changed existing tests
test/agent.test.mjs: one test renamed ("...without the confirm calls offers no buttons"), still passes.

Security: LLM text rendered as plain text; card data only from core Card(); deletion only through the existing dialog.
