package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"paratech/hub/config"
	"paratech/hub/internal/domain/llm/values"
)

// The configured auth mode and gateway path reach the provider client.
func TestLLMPartsWireTheGatewaySettings(t *testing.T) {
	const key = "test-key-not-real"
	for _, tt := range []struct {
		auth, wantX, wantAuthorization string
	}{
		{auth: config.AnthropicAuthXAPIKey, wantX: key},
		{auth: config.AnthropicAuthBearer, wantAuthorization: "Bearer " + key},
	} {
		t.Run(tt.auth, func(t *testing.T) {
			var gotPath, gotX, gotAuthorization string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotX, gotAuthorization = r.URL.Path, r.Header.Get("x-api-key"), r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m",
					"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			t.Cleanup(srv.Close)

			cfg := config.Default()
			cfg.AnthropicAPIKey, cfg.AnthropicAuth, cfg.AnthropicBaseURL, cfg.LLMTimeout = key, tt.auth, srv.URL+"/anthropic/", 10*time.Second
			parts, err := newLLMParts(cfg, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			req, err := values.NewRequest("m", "Summarize.", "data", 16)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parts.provider.Complete(t.Context(), req); err != nil {
				t.Fatal(err)
			}
			if gotPath != "/anthropic/v1/messages" || gotX != tt.wantX || gotAuthorization != tt.wantAuthorization {
				t.Fatalf("path %q, x-api-key %q, Authorization %q", gotPath, gotX, gotAuthorization)
			}
		})
	}
}
