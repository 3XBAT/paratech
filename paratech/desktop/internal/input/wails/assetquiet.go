package wails

import "net/http"

// Paths the webview asks for on its own that the app does not have.
const (
	faviconPath  = "/favicon.ico"
	customJSPath = "/wails/custom.js" // the Wails runtime probes it with HEAD (loadOptionalScript)
)

// quietAssets answers the webview's own probes with 204 instead of the 404 the console logs as an error. A 204 has
// no Content-Type, so the runtime's loadOptionalScript sees no JavaScript and appends no script, as after a 404.
func quietAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			(r.URL.Path == faviconPath || r.URL.Path == customJSPath) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
