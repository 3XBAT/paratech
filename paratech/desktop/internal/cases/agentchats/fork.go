package agentchats

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/shared"
)

// ForkResult is a new forked chat and the text of the request it was forked from, for its input (plain text, the
// upload IDs line left out).
type ForkResult struct {
	Chat Chat
	Text string
}

// Fork is «Начать заново отсюда» → «Форк» (ADR-0028): a new chat with copies of the requests of chat id before n
// (thread.ForkOf), which becomes current. Only the new chat is written; the source is only read, so a fork is allowed
// at any time, also while the source's request runs. A malformed id or n < 1 is ErrInvalidInput, a missing chat
// shared.ErrNotFound, a request that cannot be forked from thread.ErrInvalidFork.
func (s *Service) Fork(ctx context.Context, id shared.ThreadID, n int) (ForkResult, error) {
	if err := id.Validate(); err != nil {
		return ForkResult{}, fmt.Errorf("%s: %w: %w", opFork, ErrInvalidInput, err)
	}
	if n < 1 {
		return ForkResult{}, fmt.Errorf("%s: %w: request %d", opFork, ErrInvalidInput, n)
	}
	src, err := s.chats.Get(ctx, id)
	if err != nil {
		return ForkResult{}, fmt.Errorf("%s: %w", opFork, err)
	}
	past, err := s.chats.ForkSource(ctx, id, n)
	if err != nil {
		return ForkResult{}, fmt.Errorf("%s: %w", opFork, err)
	}
	f, err := thread.ForkOf(src, n, past, s.ids.NewThreadID(), s.clock.Now())
	if err != nil {
		return ForkResult{}, fmt.Errorf("%s: %w", opFork, err)
	}
	if err := s.chats.SaveFork(ctx, f); err != nil {
		return ForkResult{}, fmt.Errorf("%s: %w", opFork, err)
	}
	// ForkOf checked that request n is the last of past and has a text.
	return ForkResult{Chat: s.chatOf(ctx, f.Thread), Text: studentText(past[len(past)-1].Texts[0])}, nil
}
