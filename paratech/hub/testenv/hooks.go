package testenv

import (
	"context"
	"sync"

	"paratech/hub/internal/domain/quota/account"
)

// modifier is the Modify method of the quota account ports.
type modifier interface {
	Modify(ctx context.Context, scope account.Scope, fn func(*account.Account) error) error
}

// AfterFirstModify wraps a quota account repository (the real adapter included) and runs Hook once, right after the
// first Modify that succeeded. It puts an event, such as a client cancel, exactly between two ledger steps.
type AfterFirstModify struct {
	Accounts modifier
	Hook     func()
	once     sync.Once
}

// Modify forwards to Accounts.
func (a *AfterFirstModify) Modify(ctx context.Context, scope account.Scope, fn func(*account.Account) error) error {
	err := a.Accounts.Modify(ctx, scope, fn)
	if err == nil {
		a.once.Do(a.Hook)
	}
	return err
}
