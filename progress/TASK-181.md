# TASK-181 — agent_reply turn (history pairs), Hub

## Done
- gateway.proto: `string agent_reply = 4` in oneof `AgentTurn.turn` (additive; buf lint/breaking green, code regenerated).
- Domain `values`: `TurnAgentReply`, `AgentReplyTurn` (non-blank, UTF-8, no NUL, <= 2 KiB), order rule in `checkTurnOrder` (only between two student texts), counted in 64-turn / 256 KiB limits; fingerprint already encodes the turn kind.
- `ReplyMarkers` / `MissingReplyText` in the domain; `AgentCall.InputBytes` reserves marker bytes.
- Wire (`anthropic/wire_agent.go`): reply = assistant message with one text block; no reply in the whole request (old desktop) -> fixed assistant marker between consecutive student texts instead of merging. Echo, kept thinking turn and cache point unchanged.
- Use case and connect `turnOf` map the field. ADR-0023 addendum "History pairs (TASK-181)".

## Decisions to confirm
- The marker lives in the domain so the reservation matches what is sent.
- Markers are all-or-nothing: with at least one reply in the request, consecutive texts of different requests still merge. TASK-183 must send a reply for every past request (fixed fallback string).
- Old desktops: retried texts of the same request also get the marker (literal reading of the criterion).
- No seal for agent_reply (bounded client data, like student text).

## Deployment
Hub with this task goes out BEFORE the desktop with TASK-183 (old Hub answers invalid_argument on the unknown turn). No gateway by catalog version.

## Changed existing tests
- `shared/contract_test.go` (outside the task's file list): goldens pin every new string field; added `agent_reply`.

## Follow-ups
- TASK-183: desktop maps the turn in hubclient (build agent_reply only from the app's own validated text); audit that then.
- No Postgres e2e test in hub/tests (replay covered at use-case level).

## Security
Reviewer: approve, no blocker/high/medium. Security auditor: no findings.
