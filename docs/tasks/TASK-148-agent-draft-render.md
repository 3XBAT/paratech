# TASK-148: Progressive answer text in the agent panel (UI)

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-143, TASK-147
- **PRD sections / AC:** ADR-0022, ADR-0026 «Streaming»; ADR-0012
- **Branch:** ralph/<date>-task-148

## Goal
The answer appears progressively as a quiet draft and is replaced by the verified answer card.

## Allowed to change
- `paratech/desktop/frontend/src/agentState.ts`, `agentView.ts` (+ tests)
- `paratech/desktop/frontend/src/agent/{AgentPanel.tsx,AgentCards.tsx,agent.css}` (+ tests)

## Out of scope
- Go code; streaming of plans or replies.

## Acceptance criteria
- [ ] `agent:draft` events attach to their request (thread, turn) and call; the draft block shows caption «Ответ
      пишется · черновик» and the texts as text nodes only (never HTML), muted, with the waiting dots after the last
      line.
- [ ] The draft is removed when an update of that request brings the answer card, a stop, a problem or an end status;
      drafts of older turns are ignored.
- [ ] No scroll jumps: the panel follows the draft only if it was at the bottom; reduced motion respected.
- [ ] Tests: attach, replace by the card, removal on stop, ignore stale draft, no HTML injection.
- [ ] `npm run test` and `npm run build` pass.
