package app

import (
	"log/slog"

	"paratech/desktop/internal/adapters/urlscheme"
)

var _ schemeRegistrar = (*urlscheme.Registrar)(nil)

// schemeRegistrar keeps the paratech: link registration of the user pointing at an executable (ADR-0013).
type schemeRegistrar interface {
	Ensure(exe string) (wrote bool, err error)
}

// ensureURLScheme points the paratech: links of «Открыть Paratech» at this executable when they are missing or point
// elsewhere. It only logs, a start never fails for it, and the path (below the user's profile) is never logged.
func ensureURLScheme(reg schemeRegistrar, executable func() (string, error), log *slog.Logger) {
	if reg == nil {
		return
	}
	exe, err := executable()
	if err != nil {
		log.Warn("paratech: links are not registered: the executable path is unknown")
		return
	}
	wrote, err := reg.Ensure(exe)
	switch {
	case err != nil:
		log.Warn("paratech: links are not registered", "err", err)
	case wrote:
		log.Info("paratech: links registered for this executable")
	}
}
