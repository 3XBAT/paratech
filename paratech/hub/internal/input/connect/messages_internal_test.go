package connect

import "testing"

// TestInvalidAnswerMessageIsPinned: the desktop tells a schema refusal from a provider failure only by this text
// (desktop internal/adapters/hubclient hubMsgModelAnswer and testenv hubMsgInvalidAnswer): change both, desktop logs
// the distinction by this text.
func TestInvalidAnswerMessageIsPinned(t *testing.T) {
	const want = "the model answered, but its answer does not match the schema of the operation"
	if msgInvalidAnswer != want {
		t.Fatalf("msgInvalidAnswer = %q, want %q", msgInvalidAnswer, want)
	}
}
