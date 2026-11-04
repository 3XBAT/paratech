// Package agentcompact is the use case of context compaction of an agent chat (ADR-0028 «Context compaction»): before
// the next request of a chat whose window is full, the oldest earlier requests and the previous summary become one new
// summary made by the Hub (OPERATION_TYPE_CHAT_SUMMARY), stored on the chat. What to compact, how to shorten it to the
// Hub limits and whether the summary may be kept are rules of the thread aggregate (CompactionRequest, Compact); the
// summary text is untrusted model output, only ever sent back as the summary turn of the history (hard rule 3).
//
// One Hub call per chat at a time, chats never waiting for each other; a chat saved in between is reloaded and the summary applied again only when the chat would
// still ask for the same one (a «Скрыть» meanwhile drops it), never asked twice.
// A failure is returned as a code and stores nothing: the request then goes on without a new summary.
//
// Rules that apply: ARCH-20..27. This package does not import other use cases (ARCH-20).
package agentcompact
