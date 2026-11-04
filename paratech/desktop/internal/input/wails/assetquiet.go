package wails

import "net/http"

// Paths the webview asks for on its own that the app does not have.
const (
	faviconPath  = "/favicon.ico"
	customJSPath = "/wails/custom.js" // the Wails runtime probes it with HEAD (loadOptionalScript)
)

const quietContentType = "text/plain; charset=utf-8"

// quietAssets answers the webview's own probes with 204 instead of the 404 the console logs as an error. The 204
// carries no JavaScript content type, so the runtime's loadOptionalScript appends no script, as after a 404. The
// type is set explicitly so it does not depend on the asset server's content-type sniffer.
func quietAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			(r.URL.Path == faviconPath || r.URL.Path == customJSPath) {
			w.Header().Set("Content-Type", quietContentType)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
