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
	"time"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/cases/keyauth"
	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/internal/input/connect"
	"paratech/hub/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// anyKey is accepted by acceptAll; it is not in the issued key format.
const anyKey = "any-key"

// acceptAll is an Authenticator for router tests that are not about authentication.
type acceptAll struct{}

func (acceptAll) Authenticate(context.Context, string) (shared.APIKeyID, error) {
	return "00000000000000aa", nil
}

// failingAuth simulates a key store that is down.
type failingAuth struct{}

func (failingAuth) Authenticate(context.Context, string) (shared.APIKeyID, error) {
	return "", errors.New("db down")
}

func withKey[T any](req *connectrpc.Request[T], key string) *connectrpc.Request[T] {
	req.Header().Set("Authorization", "Bearer "+key)
	return req
}

// recordingGateway remembers the API key id that handlers see in the context.
type recordingGateway struct {
	hubv1connect.UnimplementedGatewayServiceHandler
	mu   sync.Mutex
	seen []shared.APIKeyID
}

func (g *recordingGateway) record(ctx context.Context) {
	id, ok := connect.APIKeyFrom(ctx)
	if !ok {
		id = "(none)"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seen = append(g.seen, id)
}

func (g *recordingGateway) Complete(ctx context.Context, _ *connectrpc.Request[hubv1.CompleteRequest]) (*connectrpc.Response[hubv1.CompleteResponse], error) {
	g.record(ctx)
	return connectrpc.NewResponse(&hubv1.CompleteResponse{}), nil
}

func (g *recordingGateway) CompleteStream(ctx context.Context, _ *connectrpc.Request[hubv1.CompleteStreamRequest], _ *connectrpc.ServerStream[hubv1.CompleteStreamResponse]) error {
	g.record(ctx)
	return nil
}

func (g *recordingGateway) calls() []shared.APIKeyID {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]shared.APIKeyID(nil), g.seen...)
}

// keys holds real keys generated at test time in an in-memory store.
type keys struct {
	store   *testenv.KeyStore
	valid   apikey.Credential
	revoked apikey.Credential
	foreign apikey.Credential // valid format, not stored
}

func newCredential(t *testing.T) apikey.Credential {
	t.Helper()
	idBytes, secretBytes := make([]byte, apikey.IDBytes), make([]byte, apikey.SecretBytes)
	random := testenv.Random{}
	if err := random.Fill(idBytes); err != nil {
		t.Fatal(err)
	}
	if err := random.Fill(secretBytes); err != nil {
		t.Fatal(err)
	}
	c, err := apikey.NewCredential(idBytes, secretBytes)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newKeys(t *testing.T) keys {
	t.Helper()
	k := keys{store: testenv.NewKeyStore(), valid: newCredential(t), revoked: newCredential(t), foreign: newCredential(t)}
	l, err := apikey.NewLabel("t1")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for _, c := range []apikey.Credential{k.valid, k.revoked} {
		key, err := apikey.New(c, l, at)
		if err != nil {
			t.Fatal(err)
		}
		if c == k.revoked {
			if err := key.Revoke(at); err != nil {
				t.Fatal(err)
			}
		}
		if err := k.store.Create(t.Context(), key); err != nil {
			t.Fatal(err)
		}
	}
	return k
}

func authServer(t *testing.T, auth connect.Authenticator, gw hubv1connect.GatewayServiceHandler, log *slog.Logger) *httptest.Server {
	t.Helper()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	h, err := connect.NewRouter(connect.Deps{Log: log, Readiness: fakeProbe{}, Auth: auth, Gateway: gw})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func keyAuth(t *testing.T, store *testenv.KeyStore) *keyauth.Service {
	t.Helper()
	svc, err := keyauth.New(store)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// rpc is one protected RPC called with the given Authorization header values.
type rpc struct {
	name string
	call func(ctx context.Context, srv *httptest.Server, opts []connectrpc.ClientOption, authorization []string) error
}

func setAuthorization(h http.Header, values []string) {
	for _, v := range values {
		h.Add("Authorization", v)
	}
}

var protectedRPCs = []rpc{
	{"Complete", func(ctx context.Context, srv *httptest.Server, opts []connectrpc.ClientOption, a []string) error {
		req := connectrpc.NewRequest(&hubv1.CompleteRequest{})
		setAuthorization(req.Header(), a)
		_, err := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL, opts...).Complete(ctx, req)
		return err
	}},
	{"CompleteStream", func(ctx context.Context, srv *httptest.Server, opts []connectrpc.ClientOption, a []string) error {
		req := connectrpc.NewRequest(&hubv1.CompleteStreamRequest{})
		setAuthorization(req.Header(), a)
		stream, err := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL, opts...).CompleteStream(ctx, req)
		if err != nil {
			return err
		}
		defer func() { _ = stream.Close() }()
		for stream.Receive() {
		}
		return stream.Err()
	}},
	{"GetRemaining", func(ctx context.Context, srv *httptest.Server, opts []connectrpc.ClientOption, a []string) error {
		req := connectrpc.NewRequest(&hubv1.GetRemainingRequest{})
		setAuthorization(req.Header(), a)
		_, err := hubv1connect.NewQuotaServiceClient(srv.Client(), srv.URL, opts...).GetRemaining(ctx, req)
		return err
	}},
}

var codecs = map[string][]connectrpc.ClientOption{"protobuf": nil, "json": {connectrpc.WithProtoJSON()}}

// assertRejected checks the one answer to every rejected key and returns its text for comparison.
func assertRejected(t *testing.T, err error) string {
	t.Helper()
	var ce *connectrpc.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error is %T (%v), want *connect.Error", err, err)
	}
	if ce.Code() != connectrpc.CodeUnauthenticated {
		t.Fatalf("code = %v, want unauthenticated", ce.Code())
	}
	if len(ce.Details()) != 1 {
		t.Fatalf("%d details, want 1", len(ce.Details()))
	}
	v, err := ce.Details()[0].Value()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := v.(*hubv1.ErrorDetail)
	if !ok {
		t.Fatalf("detail is %T, want *hubv1.ErrorDetail", v)
	}
	if d.GetCode() != hubv1.ErrorCode_ERROR_CODE_INVALID_API_KEY || d.GetRetryable() || d.GetRetryAfter() != nil || d.GetQuota() != nil {
		t.Fatalf("detail = %v, want INVALID_API_KEY, not retryable, nothing else", d)
	}
	return ce.Error() + "|" + d.GetMessage()
}

func TestAuthRejectsEveryBadKeyTheSameWay(t *testing.T) {
	k := newKeys(t)
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	gw := &recordingGateway{}
	srv := authServer(t, keyAuth(t, k.store), gw, log)

	valid := k.valid.Reveal()
	secretStart := len(apikey.Prefix) + 1 + 2*apikey.IDBytes + 1
	cases := map[string][]string{
		"missing header":      nil,
		"empty header":        {""},
		"wrong scheme":        {"Basic " + valid},
		"scheme only":         {"Bearer"},
		"scheme and space":    {"Bearer "},
		"two spaces":          {"Bearer  " + valid},
		"two headers":         {"Bearer " + valid, "Bearer " + valid},
		"oversized header":    {"Bearer " + valid + strings.Repeat("0", 300)},
		"malformed":           {"Bearer " + valid[:len(valid)-1]},
		"unknown id":          {"Bearer " + k.foreign.Reveal()},
		"wrong secret":        {"Bearer " + valid[:secretStart] + k.foreign.Reveal()[secretStart:]},
		"revoked":             {"Bearer " + k.revoked.Reveal()},
		"key in another case": {"Bearer " + strings.ToUpper(valid)},
	}

	var want string
	for codec, opts := range codecs {
		for _, r := range protectedRPCs {
			for name, authorization := range cases {
				t.Run(codec+"/"+r.name+"/"+name, func(t *testing.T) {
					got := assertRejected(t, r.call(t.Context(), srv, opts, authorization))
					if want == "" {
						want = got
					}
					if got != want {
						t.Fatalf("answer %q differs from %q: the client could tell the cases apart", got, want)
					}
				})
			}
		}
	}
	if calls := gw.calls(); len(calls) != 0 {
		t.Fatalf("handlers ran for rejected keys: %v", calls)
	}

	// Logs carry the reason but no key material: not the key, its secret or even its id.
	for _, c := range []apikey.Credential{k.valid, k.revoked, k.foreign} {
		key := c.Reveal()
		for _, part := range []string{key, key[secretStart:], key[secretStart : secretStart+16], c.ID().String()} {
			if strings.Contains(strings.ToLower(logged.String()), part) {
				t.Fatalf("the log holds key material %q:\n%s", part[:4]+"...", logged.String())
			}
		}
	}
	for _, reason := range []string{"malformed", "unknown", "revoked", "wrong secret", "missing or malformed Authorization header"} {
		if !strings.Contains(logged.String(), reason) {
			t.Errorf("no log line with reason %q", reason)
		}
	}
}

func TestAuthLetsAValidKeyThrough(t *testing.T) {
	k := newKeys(t)
	valid := k.valid.Reveal()

	t.Run("unimplemented handlers answer", func(t *testing.T) {
		srv := authServer(t, keyAuth(t, k.store), nil, nil)
		for codec, opts := range codecs {
			for _, r := range protectedRPCs {
				for _, scheme := range []string{"Bearer ", "bearer ", "BEARER "} {
					err := r.call(t.Context(), srv, opts, []string{scheme + valid})
					if connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
						t.Errorf("%s/%s with %q: err = %v, want unimplemented from the handler", codec, r.name, scheme, err)
					}
				}
			}
		}
	})

	t.Run("handlers see the key id", func(t *testing.T) {
		gw := &recordingGateway{}
		srv := authServer(t, keyAuth(t, k.store), gw, nil)
		for _, r := range protectedRPCs[:2] { // Complete and CompleteStream reach the recording gateway
			if err := r.call(t.Context(), srv, nil, []string{"Bearer " + valid}); err != nil {
				t.Fatalf("%s: %v", r.name, err)
			}
		}
		calls := gw.calls()
		if len(calls) != 2 || calls[0] != k.valid.ID() || calls[1] != k.valid.ID() {
			t.Fatalf("handlers saw %v, want the key id twice", calls)
		}
	})
}

func TestAuthStoreFailureIsUnavailable(t *testing.T) {
	srv := authServer(t, failingAuth{}, nil, nil)
	for _, r := range protectedRPCs {
		err := r.call(t.Context(), srv, nil, []string{"Bearer " + anyKey})
		var ce *connectrpc.Error
		if !errors.As(err, &ce) || ce.Code() != connectrpc.CodeUnavailable || len(ce.Details()) != 0 {
			t.Fatalf("%s: err = %v, want unavailable without detail", r.name, err)
		}
		if strings.Contains(ce.Message(), "db down") {
			t.Fatalf("%s: the internal error reaches the client: %v", r.name, ce)
		}
	}
}

func TestAPIKeyFromOutsideARequest(t *testing.T) {
	if id, ok := connect.APIKeyFrom(t.Context()); ok || id != "" {
		t.Fatalf("APIKeyFrom(empty context) = %q, %v", id, ok)
	}
}

func TestProbesAndInternalRPCsIgnoreAuth(t *testing.T) {
	srv := authServer(t, keyAuth(t, newKeys(t).store), nil, nil)

	for _, p := range []string{connect.PathHealthz, connect.PathReadyz} {
		if code, _ := get(t, srv.URL+p); code != http.StatusOK {
			t.Errorf("%s without a key = %d, want 200", p, code)
		}
	}
	for _, p := range []string{
		hubv1connect.QuotaServiceReserveProcedure,
		hubv1connect.QuotaServiceSettleProcedure,
		hubv1connect.QuotaServiceReleaseProcedure,
	} {
		if code := postJSON(t, srv.URL+p, "{}", ""); code != http.StatusNotFound {
			t.Errorf("%s: HTTP status = %d, want 404", p, code)
		}
	}
	// A rejected key is HTTP 401 for plain HTTP clients.
	if code := postJSON(t, srv.URL+hubv1connect.QuotaServiceGetRemainingProcedure, "{}", ""); code != http.StatusUnauthorized {
		t.Errorf("GetRemaining without a key: HTTP status = %d, want 401", code)
	}
}
