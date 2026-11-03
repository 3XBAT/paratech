# frontend/src

React 19 + TypeScript UI of the desktop app, built by Vite into the Go package that embeds it
(`paratech/desktop/internal/input/wails/assets/app/`, git-ignored; `vite.config.ts`). It has two entry points: the main
window (`index.html` -> `main.tsx` -> `App.tsx`) and the recording popup (`popup.html` -> `popup/main.tsx`). The UI holds no
business rules and no secrets: every read and action is a call to a Go service bound in
`paratech/desktop/internal/input/wails` (ARCH-44). Paths below are relative to this folder unless they start with `paratech/` or `docs/`.

## Map
- `App.tsx` - the window: rail, current page, agent panel, lesson screen over a page, one-shot "open this with that" state.
- `nav.ts` (`PageId`, `pages`, `rail`, settings menu order, `Route` helpers `routeTo*`), `navHistory.ts`, `useNavHistory.ts`
  (Alt+Left/Right history), `recipientNav.ts`, `openLesson.ts`: routing. There is no router library; the route is state in `App`.
- `pages/` - screens (`*Page.tsx`) and their parts (`*Parts.tsx`, dialogs, rows), plus page hooks (`use*.ts`) and page CSS.
- `*View.ts` (also `workView.ts`, `agentView.ts`, `todayView.ts`...) in this folder - pure view-model logic: Russian texts
  (`texts`), state mapping, gating, formatting. No React state, no DOM, no Wails calls; unit tests sit in `../test`.
  `statusPresentation.ts` / `statusWords.ts` map core status codes to chips and words; `plural.ts`, `clock.ts` are helpers.
- `bindings/` - hand-written typed wrappers over the Go services (not generated). One file per service:
  `methods` table with full Go names, `parse*` validators of untrusted replies, `xxxApi(call)` factory.
  `bindings/runtime.ts` is the only place that touches the Wails runtime (`callByName`, `onEvent`, `onEventData`).
- `ui/` - shared components (`Button`, `IconAction`, `Rail`, `Icon`, `controls.tsx`, `Toast`, `Breadcrumbs`), theme/accent, tokens CSS.
- `agent/` - the agent panel and its cards: see `agent/README.md`.
- `popup/` - the popup window (`Popup.tsx`, `view.ts`, `joinView.ts`, `popup.css`); it must not take focus on its own (AC-01).
- `sourcePicker/` - audio-source picker shared by the popup, `pages/RecordingsPage.tsx` and `pages/DiagnosticsPage.tsx`.
- `styles.css` - global layout; other CSS is imported in `main.tsx` (`popup/main.tsx` for the popup) or by the component.

How a page works: `pages/XPage.tsx` builds `xApi(callByName)` from `bindings/x.ts`, keeps React state, and renders what
`xView.ts` computes (see `pages/TicketsPage.tsx` with `ticketsView.ts` and `bindings/tickets.ts`).

## Where to add
- Page: `bindings/x.ts` -> `xView.ts` -> `pages/XPage.tsx` -> `nav.ts` (`PageId`, `pages`, a `rail` item's `covers`, or
  `settingsMainPages`/`settingsAdvancedPages`; a new rail icon goes in `ui/Icon.tsx`) -> `case` in `PageView` of `App.tsx`
  -> `test/x.test.mjs` and its entry in `../tsconfig.test.json`. `test/nav.test.mjs` pins the rail and the page list.
- Binding: first the Go side - a service in `paratech/desktop/internal/input/wails/*.go`, a field in `Services`/`OpenServices`
  of `shell.go`, `RegisterService` in `shell_windows.go`. Then `bindings/x.ts` with the name
  `paratech/desktop/internal/input/wails.<Type>.<Method>` (as in `bindings/tickets.ts`) and a `parse*` for each reply.
- UI component: `ui/` (use `Button` for main actions, `IconAction` for small ones), styles in `ui/components.css` or its own CSS.
- Agent card or message action: see `agent/README.md`.

## Rules that bind
- ARCH-44 (no business rules or secrets in the UI), ARCH-40..43 (the Go side of the bindings), ARCH-27 and CLAUDE.md rule 3
  (model output is untrusted). Model, file and student text render as React text nodes; `src` has no `innerHTML`.
- UI texts are Russian, impersonal voice. Side rail: icons only, name as a hover/focus tooltip (`ui/Rail.tsx`); small and
  secondary buttons are icons with a tooltip and `aria-label` (`ui/IconAction.tsx`, TASK-368), words stay on main actions.
- Navigation and settings structure: ADR-0034. Visual direction: ADR-0012, `docs/design/brief.md`.
- Agent parity: a user-facing feature the UI gets must be doable by the agent too, or the task records why not (ADR-0033 §1).

## Tests
`npm --prefix paratech/desktop/frontend run test` = `tsc -p tsconfig.test.json` (compiles the files listed in its `files`
array, and what they import, into `.test-build/`) then `node --test "test/*.test.mjs"`. `run build` = `tsc -p tsconfig.json && vite build`.
Tests are `test/<topic>.test.mjs` (some carry the task number, `breadcrumbs356.test.mjs`), plain `node:test`, importing from
`../.test-build/...`; components are rendered with `react-dom/server`, there is no DOM and no jsdom.

## Gotchas
- A new module that a test imports but no listed file reaches is not compiled: add it to `files` in `tsconfig.test.json`.
- Modules under test import siblings with a `.js` suffix (`./nav.js`) so Node can load the compiled output; Vite maps it back.
- Many tests read source text (`readFileSync` of `.tsx`, `.css`, even Go files such as `internal/input/wails/agent.go`) and
  match it with regexes: renaming a class, file or Go method can fail a test far from your change.
- `localStorage` is used only for view preferences (`ui/theme.ts`, `ui/accent.ts`, `agent/panelWidth.ts`, `subjectsHome.ts`).
- The header comment of `nav.ts` still says the rail has a label under the icon; `ui/Rail.tsx` and `test/nav.test.mjs` say icon-only.
- `run build` rewrites the git-ignored `assets/app/` of the Go package; never stage it.

## E2E
ADR-0035 plans real-app Playwright packs in `paratech/e2e/specs/<pack>/` (settings and connections, recording, popup and
capture, assignments, agent, layout). That directory does not exist yet (only `paratech/e2e/src` and `paratech/e2e/test`
do). Per ADR-0035 §6 a UI task will add or update its scenario in its pack; `docs/process/e2e.md` is not written yet.
