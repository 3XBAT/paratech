package wails

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQuietAssets(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	tests := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/favicon.ico", http.StatusNoContent},
		{http.MethodHead, "/favicon.ico", http.StatusNoContent},
		{http.MethodGet, "/wails/custom.js", http.StatusNoContent},
		{http.MethodHead, "/wails/custom.js", http.StatusNoContent},
		{http.MethodPost, "/favicon.ico", http.StatusTeapot},
		{http.MethodGet, "/wails/runtime.js", http.StatusTeapot},
		{http.MethodGet, "/index.html", http.StatusTeapot},
		{http.MethodGet, "/assets/favicon.ico", http.StatusTeapot},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		quietAssets(next).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
		if rec.Code != tt.want || rec.Body.Len() != 0 {
			t.Errorf("%s %s = %d %q, want %d", tt.method, tt.path, rec.Code, rec.Body.String(), tt.want)
		}
		// loadOptionalScript appends a script only for a JavaScript content type.
		if tt.want == http.StatusNoContent && rec.Header().Get("Content-Type") != "" {
			t.Errorf("%s %s Content-Type = %q, want none", tt.method, tt.path, rec.Header().Get("Content-Type"))
		}
	}
}
