package googleauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"paratech/desktop/testenv"
)

// echo marks every request value; no answer page may contain it.
const echo = "ECHO7f3a"

// Every answer of the redirect handler is one of the constant pages with the full set of headers, and none of them
// carries a request value back.
func TestCallbackPages(t *testing.T) {
	const host = "127.0.0.1:1"
	const state = "s" + echo
	tests := []struct {
		name   string
		method string
		target string
		host   string
		status int
		page   string
	}{
		{name: "code", target: "/?code=" + echo + "%3Cscript%3E&state=" + state, status: http.StatusOK, page: pageDone},
		{name: "denied", target: "/?error=access_denied&error_description=" + echo + "&state=" + state,
			status: http.StatusOK, page: pageDenied},
		{name: "other error", target: "/?error=" + echo + "%22%3E&error_description=" + echo + "&error_uri=http://x/" + echo +
			"&state=" + state, status: http.StatusOK, page: pageFailed},
		{name: "wrong state", target: "/?code=" + echo + "&state=" + echo + "x", status: http.StatusBadRequest, page: pageFailed},
		{name: "code and error", target: "/?code=" + echo + "&error=" + echo + "&state=" + state, status: http.StatusBadRequest, page: pageFailed},
		{name: "other path", target: "/" + echo + "?code=" + echo + "&state=" + state, status: http.StatusNotFound, page: pageFailed},
		{name: "post", method: http.MethodPost, target: "/?code=" + echo + "&state=" + state, status: http.StatusMethodNotAllowed, page: pageFailed},
		{name: "other host", target: "/?code=" + echo + "&state=" + state, host: echo + ".example:1", status: http.StatusBadRequest, page: pageFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := &callback{state: state, host: host, result: make(chan redirectResult, 1)}
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tt.target, nil)
			req.Host = host
			if tt.host != "" {
				req.Host = tt.host
			}
			rec := httptest.NewRecorder()
			cb.ServeHTTP(rec, req)

			if rec.Code != tt.status || rec.Body.String() != tt.page {
				t.Fatalf("answer %d %q, want %d and the page", rec.Code, rec.Body, tt.status)
			}
			if strings.Contains(rec.Body.String(), echo) || strings.Contains(strings.Join(headerValues(rec.Header()), " "), echo) {
				t.Fatal("the answer echoes the request")
			}
			want := map[string]string{
				"Content-Type": "text/html; charset=utf-8", "X-Content-Type-Options": "nosniff", "Cache-Control": "no-store",
				"Referrer-Policy": "no-referrer", "X-Frame-Options": "DENY",
				"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
			}
			for k, v := range want {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
		})
	}
}

func headerValues(h http.Header) []string {
	var out []string
	for k, vs := range h {
		out = append(out, k)
		out = append(out, vs...)
	}
	return out
}

// The pages are self-contained Russian pages in the app's style: a title, an icon, the next step, «Открыть Paratech»
// and both colour schemes, without scripts, external resources or fonts to load.
func TestPagesAreSelfContained(t *testing.T) {
	tests := []struct {
		name string
		page string
		want []string
	}{
		{name: "done", page: pageDone, want: []string{"<h1>Вход выполнен</h1>", "Google ответил.", "результат входа показан там"}},
		{name: "denied", page: pageDenied, want: []string{"<h1>Доступ не разрешён</h1>", "Вы не разрешили доступ", "«Войти через Google»"}},
		{name: "failed", page: pageFailed, want: []string{"<h1>Вход не выполнен</h1>", "истекло время ожидания", "«Войти через Google»"}},
	}
	common := []string{
		`<html lang="ru">`, `<meta charset="utf-8">`, "<title>Paratech: ", "<svg ", "Можно закрыть эту вкладку и вернуться в приложение",
		`<a class="btn" href="paratech://open">Открыть Paratech</a>`, "@media (prefers-color-scheme:dark)", `"Golos Text"`, "sans-serif",
	}
	forbidden := []string{"<script", "javascript:", "@font-face", "@import", "url(", "src=", "http:", "https:", "data:", "<img", "<link",
		"<form", "<iframe", "<base", "onclick", "onload"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, w := range append(common, tt.want...) {
				if !strings.Contains(tt.page, w) {
					t.Errorf("the page lacks %q", w)
				}
			}
			lower := strings.ToLower(tt.page)
			for _, f := range forbidden {
				if strings.Contains(lower, f) {
					t.Errorf("the page contains %q", f)
				}
			}
			if n := strings.Count(tt.page, "href="); n != 1 {
				t.Errorf("%d links, want only the app button", n)
			}
		})
	}
}

// A refused consent screen ends with the «denied» page in the browser.
func TestSignInDeniedPage(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	r := newRig(t)
	r.google.Deny(true)
	if err := r.a.SignIn(context.Background()); !errors.Is(err, ErrDenied) {
		t.Fatalf("SignIn = %v, want ErrDenied", err)
	}
	visits := r.browser.Visits()
	if len(visits) != 1 || visits[0].Status != http.StatusOK || visits[0].Body != pageDenied {
		t.Fatalf("visits %+v, want the denied page", visits)
	}
}
