package connect

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	connectrpc "connectrpc.com/connect"
	"github.com/go-chi/chi/v5"

	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

const (
	// Paths of the probes.
	PathHealthz = "/healthz"
	PathReadyz  = "/readyz"

	// maxRequestBytes bounds one Connect message: the 1 MiB payload limit of ADR-0005(b) plus envelope headroom.
	maxRequestBytes = 1<<20 + 64<<10

	// readyTimeout bounds one readiness check, so a hanging database gives 503 instead of a hanging probe.
	readyTimeout = 2 * time.Second
)

// ReadinessProbe reports whether the Hub can serve requests (the database answers).
type ReadinessProbe interface {
	Ping(ctx context.Context) error
}

// Deps are what the router serves. A nil service is served by the generated Unimplemented handler, which answers
// every RPC with CodeUnimplemented, until the gateway and quota tasks provide real ones.
//
// TODO(TASK-032): every RPC must pass the API-key interceptor before real handlers are mounted.
type Deps struct {
	Log       *slog.Logger
	Readiness ReadinessProbe
	Gateway   hubv1connect.GatewayServiceHandler
	Quota     hubv1connect.QuotaServiceHandler
}

// NewRouter builds the HTTP handler of the public listener.
func NewRouter(d Deps) (http.Handler, error) {
	if d.Log == nil || d.Readiness == nil {
		return nil, errors.New("connect: logger and readiness probe are required")
	}
	if d.Gateway == nil {
		d.Gateway = hubv1connect.UnimplementedGatewayServiceHandler{}
	}
	if d.Quota == nil {
		d.Quota = hubv1connect.UnimplementedQuotaServiceHandler{}
	}

	r := chi.NewRouter()
	r.Get(PathHealthz, healthz)
	r.Get(PathReadyz, readyz(d.Log, d.Readiness))

	opts := []connectrpc.HandlerOption{connectrpc.WithReadMaxBytes(maxRequestBytes)}
	path, h := hubv1connect.NewGatewayServiceHandler(d.Gateway, opts...)
	r.Mount(path, h)
	path, h = hubv1connect.NewQuotaServiceHandler(d.Quota, opts...)
	r.Mount(path, publicQuota(h))

	return r, nil
}

// publicQuota lets through only the public QuotaService RPCs (ADR-0005(f)). Later tasks must keep internal RPCs off
// this listener; shared/contract_test.go pins which RPCs are public.
func publicQuota(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case hubv1connect.QuotaServiceGetRemainingProcedure:
			next.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// healthz is the liveness probe: the process serves HTTP.
func healthz(w http.ResponseWriter, _ *http.Request) {
	writeProbe(w, http.StatusOK, "ok")
}

// readyz is the readiness probe. The body never carries the failure reason (it may name hosts or users); the reason
// goes to the log.
func readyz(log *slog.Logger, probe ReadinessProbe) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()

		if err := probe.Ping(ctx); err != nil {
			log.WarnContext(ctx, "readiness check failed", "err", err)
			writeProbe(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		writeProbe(w, http.StatusOK, "ok")
	}
}

func writeProbe(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body + "\n"))
}
