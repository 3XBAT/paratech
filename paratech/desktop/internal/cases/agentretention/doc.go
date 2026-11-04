// Package agentretention is the use case that purges the technical details of earlier agent requests (TASK-250,
// ADR-0028 «Retention»): the sealed calls and the tool results of every request that is final, not the current one of
// its chat, and ended more than thread.DetailsRetention ago. The chats, the requests, the student's texts, the summary
// and the action journal are kept forever. A request without a frozen reply line gets it first (thread.FrozenReply of
// its entries, the line the window built from them), in the same transaction as the delete; a request whose entries
// cannot be read or whose line is not valid keeps everything.
//
// It runs at the start and once a day, in bounded batches; a request done no longer qualifies, so a run that stops
// anywhere is finished by the next one.
//
// Rules that apply: ARCH-20..27.
package agentretention
