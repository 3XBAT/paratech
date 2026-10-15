package connect_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/cases/quota"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/internal/input/connect"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// fakeQuota is a QuotaReader that remembers who asked.
type fakeQuota struct {
	mu       sync.Mutex
	snapshot quota.Snapshot
	err      error
	owners   []shared.APIKeyID
}

func (f *fakeQuota) GetRemaining(_ context.Context, owner shared.APIKeyID) (quota.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners = append(f.owners, owner)
	return f.snapshot, f.err
}

func quotaServer(t *testing.T, svc connect.QuotaReader, log *slog.Logger) *httptest.Server {
	t.Helper()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	q, err := connect.NewQuota(svc, log)
	if err != nil {
		t.Fatal(err)
	}
	h, err := connect.NewRouter(connect.Deps{Log: log, Readiness: fakeProbe{}, Auth: acceptAll{}, Quota: q})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestNewQuotaRequiresDeps(t *testing.T) {
	if _, err := connect.NewQuota(nil, slog.New(slog.DiscardHandler)); err == nil {
		t.Error("no error without a service")
	}
	if _, err := connect.NewQuota(&fakeQuota{}, nil); err == nil {
		t.Error("no error without a logger")
	}
}

func TestGetRemainingReturnsTheCallersSnapshot(t *testing.T) {
	svc := &fakeQuota{snapshot: quota.Snapshot{Unit: "tokens", Remaining: 700, Reserved: 300, Limit: 1000}}
	srv := quotaServer(t, svc, nil)

	for codec, opts := range codecs {
		client := hubv1connect.NewQuotaServiceClient(srv.Client(), srv.URL, opts...)
		resp, err := client.GetRemaining(t.Context(), withKey(connectrpc.NewRequest(&hubv1.GetRemainingRequest{}), anyKey))
		if err != nil {
			t.Fatalf("%s: %v", codec, err)
		}
		q := resp.Msg.GetQuota()
		tokens := hubv1.QuotaUnit_QUOTA_UNIT_TOKENS
		if q.GetRemaining().GetValue() != 700 || q.GetReserved().GetValue() != 300 || q.GetLimit().GetValue() != 1000 ||
			q.GetRemaining().GetUnit() != tokens || q.GetReserved().GetUnit() != tokens || q.GetLimit().GetUnit() != tokens ||
			q.GetPeriodEndsAt() != nil {
			t.Fatalf("%s: snapshot = %v", codec, q)
		}
	}
	// The use case was asked for the authenticated key (acceptAll's id), not for anything the client sent.
	for _, owner := range svc.owners {
		if owner != "00000000000000aa" {
			t.Fatalf("GetRemaining asked for %q", owner)
		}
	}
	if len(svc.owners) != len(codecs) {
		t.Fatalf("%d calls, want %d", len(svc.owners), len(codecs))
	}
}

func TestGetRemainingStoreFailureIsUnavailable(t *testing.T) {
	var logged bytes.Buffer
	srv := quotaServer(t, &fakeQuota{err: errors.New("dial db.internal:5432")}, slog.New(slog.NewTextHandler(&logged, nil)))
	client := hubv1connect.NewQuotaServiceClient(srv.Client(), srv.URL)
	_, err := client.GetRemaining(t.Context(), withKey(connectrpc.NewRequest(&hubv1.GetRemainingRequest{}), anyKey))
	var ce *connectrpc.Error
	if !errors.As(err, &ce) || ce.Code() != connectrpc.CodeUnavailable || len(ce.Details()) != 0 {
		t.Fatalf("err = %v, want unavailable without detail", err)
	}
	if strings.Contains(ce.Message(), "db.internal") {
		t.Fatalf("the internal error reaches the client: %v", ce)
	}
	if !strings.Contains(logged.String(), "db.internal") {
		t.Fatalf("the reason is not logged: %s", logged.String())
	}
}

func TestGetRemainingUnknownUnitIsInternal(t *testing.T) {
	srv := quotaServer(t, &fakeQuota{snapshot: quota.Snapshot{Unit: "credits", Limit: 1}}, nil)
	client := hubv1connect.NewQuotaServiceClient(srv.Client(), srv.URL)
	_, err := client.GetRemaining(t.Context(), withKey(connectrpc.NewRequest(&hubv1.GetRemainingRequest{}), anyKey))
	if connectrpc.CodeOf(err) != connectrpc.CodeInternal {
		t.Fatalf("err = %v, want internal", err)
	}
}

func TestGetRemainingWithoutIdentityFailsClosed(t *testing.T) {
	svc := &fakeQuota{}
	q, err := connect.NewQuota(svc, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	_, err = q.GetRemaining(t.Context(), connectrpc.NewRequest(&hubv1.GetRemainingRequest{}))
	if connectrpc.CodeOf(err) != connectrpc.CodeUnauthenticated || len(svc.owners) != 0 {
		t.Fatalf("err = %v, calls %d; want unauthenticated without a call", err, len(svc.owners))
	}
}

// TestQuotaHandlerKeepsInternalRPCsOff: with the real handler mounted, Reserve, Settle and Release are still not routed,
// and the handler itself does not implement them.
func TestQuotaHandlerKeepsInternalRPCsOff(t *testing.T) {
	srv := quotaServer(t, &fakeQuota{snapshot: quota.Snapshot{Unit: "tokens"}}, nil)
	for _, p := range []string{
		hubv1connect.QuotaServiceReserveProcedure,
		hubv1connect.QuotaServiceSettleProcedure,
		hubv1connect.QuotaServiceReleaseProcedure,
	} {
		for _, authorization := range []string{"", "Bearer " + anyKey} {
			if code := postJSON(t, srv.URL+p, "{}", authorization); code != http.StatusNotFound {
				t.Errorf("%s (authorization %t): HTTP status = %d, want 404", p, authorization != "", code)
			}
		}
	}
	if code := postJSON(t, srv.URL+hubv1connect.QuotaServiceGetRemainingProcedure, "{}", "Bearer "+anyKey); code != http.StatusOK {
		t.Errorf("GetRemaining: HTTP status = %d, want 200", code)
	}

	q, err := connect.NewQuota(&fakeQuota{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Reserve(t.Context(), connectrpc.NewRequest(&hubv1.ReserveRequest{})); connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
		t.Errorf("Reserve = %v, want unimplemented", err)
	}
	if _, err := q.Settle(t.Context(), connectrpc.NewRequest(&hubv1.SettleRequest{})); connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
		t.Errorf("Settle = %v, want unimplemented", err)
	}
	if _, err := q.Release(t.Context(), connectrpc.NewRequest(&hubv1.ReleaseRequest{})); connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
		t.Errorf("Release = %v, want unimplemented", err)
	}
}
