//go:build !windows

package app

import (
	"context"
	"errors"
	"testing"
)

// Off Windows the production credential stores refuse every call: there is no weaker fallback (hard rule 4). Only
// built off Windows, so no app test ever touches the real Credential Manager.
func TestProductionSecretsHaveNoFallbackOffWindows(t *testing.T) {
	ctx := context.Background()
	for _, service := range []string{hubSecretsService, mailSecretsService} {
		store, err := productionEnv().secrets(service)
		if err != nil {
			t.Fatalf("%s: %v", service, err)
		}
		if _, err := store.Get(ctx, "any-name"); !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s: Get = %v, want errors.ErrUnsupported", service, err)
		}
		if err := store.Set(ctx, "any-name", "secret"); !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s: Set = %v, want errors.ErrUnsupported", service, err)
		}
	}
}
