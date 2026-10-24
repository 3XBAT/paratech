// Package answering is the use case of answers with citations (TASK-118, ADR-0019, ADR-0022): a student's question is
// answered from their own lessons. The knowledge base is scanned for the passages that match the question, they go
// as fragments to the quarantined reader ANSWER on the Hub, its answer is parsed strictly and every citation is
// checked against its fragment (coursework/answer). The answer is stored, so its card is read by ID, and the same
// question about the same fragments is answered from the store, free.
//
// The fragments and the reader's answer are untrusted data: nothing here acts on them. The agent's planner gets only
// the answer ID and counts (input/agent); the text goes to the card only.
package answering
