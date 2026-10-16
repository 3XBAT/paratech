package googleauth

// pagePolicy allows only the page's own inline <style>: no scripts, fonts, images, frames, forms or base URL. The
// icons are inline SVG markup, which no fetch directive governs.
const pagePolicy = "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// openAppURL is the button that brings the app's main window forward (docs/adr/0013-paratech-url-scheme.md).
const openAppURL = "paratech://open"

// The answer pages of the redirect (Russian UI): constant strings that never echo anything of the request. Colours are
// the tokens of desktop/frontend/src/ui/tokens.css (ADR-0012); the font is named with a system fallback and never
// loaded, so the page makes no request at all.
const (
	pageDone = pageHead + `<title>Paratech: вход выполнен</title></head><body><main class="card">` +
		`<div class="icon"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5"/></svg></div>` +
		`<h1>Вход выполнен</h1>` +
		`<p>Google подтвердил вход. Можно закрыть эту вкладку и вернуться в приложение.</p>` +
		pageButton + `</main></body></html>`

	pageDenied = pageHead + `<title>Paratech: доступ не разрешён</title></head><body><main class="card deny">` +
		`<div class="icon"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18"/></svg></div>` +
		`<h1>Доступ не разрешён</h1>` +
		`<p>Вы не разрешили доступ к Google Календарю, поэтому приложение к нему не подключено. ` +
		`Чтобы подключить, нажмите «Войти через Google» в разделе «Подключения» ещё раз.</p>` +
		pageButton + pageClose + `</main></body></html>`

	pageFailed = pageHead + `<title>Paratech: вход не выполнен</title></head><body><main class="card fail">` +
		`<div class="icon"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 4l9 16H3zM12 10v4M12 17h.01"/></svg></div>` +
		`<h1>Вход не выполнен</h1>` +
		`<p>Возможно, истекло время ожидания или эта вкладка осталась от прошлой попытки. ` +
		`Нажмите «Войти через Google» в разделе «Подключения» ещё раз.</p>` +
		pageButton + pageClose + `</main></body></html>`
)

// Shared parts of the pages.
const (
	pageButton = `<a class="btn" href="` + openAppURL + `">Открыть Paratech</a>`
	pageClose  = `<p class="m">Можно закрыть эту вкладку и вернуться в приложение.</p>`

	pageHead = `<!doctype html><html lang="ru"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light dark">` +
		`<style>` +
		`:root{--bg:#EDEDEA;--panel:#FAFAF8;--line:#E2E2DD;--text:#1A1B1E;--muted:#5A5D64;--accent:#A6541E;` +
		`--prim:#1F2023;--on-prim:#FAFAF8;--c:#2C6644;--b:#E1EEE5;` +
		`--sh:0 1px 2px rgba(20,20,24,.05),0 18px 44px -22px rgba(20,20,24,.28);color-scheme:light}` +
		`.deny{--c:#735000;--b:#F4ECD4}.fail{--c:#9C3027;--b:#F7E5E2}` +
		`@media (prefers-color-scheme:dark){` +
		`:root{--bg:#131416;--panel:#1B1C1F;--line:#2C2E32;--text:#ECEBE7;--muted:#9EA1A8;--accent:#DE9A68;` +
		`--prim:#E6E4DF;--on-prim:#16171A;--c:#8FCDA7;--b:#1C2F25;` +
		`--sh:0 1px 2px rgba(0,0,0,.4),0 22px 48px -24px rgba(0,0,0,.85);color-scheme:dark}` +
		`.deny{--c:#DFC07A;--b:#312B1C}.fail{--c:#F0958B;--b:#3A2422}}` +
		`*{box-sizing:border-box}` +
		`body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px;background:var(--bg);color:var(--text);` +
		`font:14px/1.5 "Golos Text","Segoe UI Variable Text","Segoe UI",system-ui,sans-serif;-webkit-font-smoothing:antialiased}` +
		`.card{width:100%;max-width:420px;padding:32px 28px 28px;text-align:center;background:var(--panel);` +
		`border:1px solid var(--line);border-radius:14px;box-shadow:var(--sh)}` +
		`.icon{width:56px;height:56px;margin:0 auto 16px;display:grid;place-items:center;border-radius:50%;` +
		`color:var(--c);background:var(--b);border:1px solid color-mix(in srgb,var(--c) 40%,transparent)}` +
		`svg{width:28px;height:28px;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}` +
		`h1{margin:0 0 8px;font-size:22px;line-height:28px;font-weight:600;letter-spacing:-.02em}` +
		`p{margin:0 0 20px;color:var(--muted)}` +
		`.btn{display:inline-flex;align-items:center;justify-content:center;min-height:36px;padding:7px 18px;` +
		`border-radius:8px;background:var(--prim);color:var(--on-prim);font-weight:500;text-decoration:none}` +
		`.btn:hover{opacity:.9}.btn:focus-visible{outline:2px solid var(--accent);outline-offset:2px}` +
		`.m{margin:16px 0 0;font-size:12.5px}` +
		`</style>`
)
