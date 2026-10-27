# TASK-223 — long agent answers (catalog 7)

## Done
- Catalog 7 (shared/testdata/tool_catalog_v7.json, hub/pkg/prompts/agent/panel/v7): new arg type `reply_text` for `reply.text`, up to 4000 characters, plain text, paragraphs on empty lines, `<` `>` allowed. Other card texts keep 300. Catalogs 1-6 unchanged.
- Prompt v7: short answer for a simple question, long structured by paragraphs when asked to explain or the question is big.
- Hub: text-only answer (TextReply) and a returned `reply` over 4000 are cut at a sentence end with an ellipsis (rune-safe) instead of the old charged "no tool use"; also cut when arguments exceed 8 KiB (3-byte scripts). Cut calls are sealed, stored, replayed; the echo works. `reply_cut=true` in the log, never the text.
- Review fix: the anthropic adapter accepts returned arguments up to 32 KiB (`NewReturnedToolCall`); `CheckReturned` enforces 8 KiB for every call, so long Cyrillic replies (~2 bytes/char) are cut instead of failing. Over 32 KiB stays a charged PROVIDER_ERROR.
- Desktop: CatalogVersion 7, `ArgReplyText`, `ParseReplyText` (4000 runes, printable plus line breaks, `<` `>` allowed), ReplyCard validation; history `agent_reply` still cut to 2 KiB.
- Frontend: panel already renders text nodes with pre-wrap and a scrolling thread; only a test was added (agentlongreply.test.mjs).
- ADR 0023 addendum (long replies, deployment order: Hub with catalog 7 first).

## Decisions to confirm
- Owner's «Обрезать НЕ надо» read as: never cut up to 4000, cut only above it (the task also asks the sentence cut).
- Angle brackets accepted in `reply` under catalog 7 (stored unescaped; safe only while the UI renders text). Tabs still refused by desktop and Hub text-only path.
- `MaxOutputTokens` stays 4096 (Hub planner alias cap 4096): a 4000-character Russian answer plus thinking may hit the cap; raising needs a Hub config change.
- Fixture shared/testdata/agent/replay/18-html-in-card.json moved HTML from `reply` to `cannot_do` (reply HTML is now accepted by design).

## Follow-ups
- Raise the planner output cap for catalog 7 if long answers hit max_tokens.
- Log reason for an oversized call on catalogs 1-6 is now "invalid tool calls" (was "malformed"); same charge and client error.
- Optional replay fixture for angle brackets in a reply (fixtures carry no catalog version).
- Live agenteval of the v7 prompt (needs a key).

## Changed existing tests
- prompts_test (7 versions); shared/contract_test (current catalog 7, v5 frozen compared with v6 file); values/tool_test (version 7); args_test, driver_test, wails agent_test, agent_injection_test (reply now accepts long text and `<` `>`; the 300/HTML checks moved to show_plan/cannot_do); anthropic agent_test (huge input uses the 32 KiB bound); hub/tests agent_thinking_test helper refactor (behaviour unchanged).

## Security
Reviewer: one medium (8 KiB intake) fixed, re-review approved. Auditor: no findings. Invariant to keep: reply text is rendered as text only, never HTML.
