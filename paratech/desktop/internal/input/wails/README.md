# input/wails

The Windows UI shell of the desktop core (main window, tray, recording popup) on Wails v3, and every binding the
React pages call. Each service (e.g. `TicketService`) is a thin binding: it declares narrow interfaces on the use cases it calls (ARCH-42),
maps their results to JSON DTOs and their errors to fixed Russian texts. The per-page tour is in `doc.go`.
File names below are in this package unless a path is given.

## Map
- `shell.go`: `Options`, `Core` (what `paratech/desktop/internal/app` hands in), `Services`, `Shell.OpenServices` (nil checks and
  constructors), the dialog and clipboard forwarders. `shell_windows.go`: `application.New`, the
  `app.RegisterService` list, windows, tray, file drops. `shell_other.go`: the non-Windows stub (`ErrUnsupported`).
- `*_windows.go`: the only Wails code (ARCH-33); `*picker_windows.go` are the native open/save dialogs.
- `spec.go`, `popupwindow.go`, `icon.go`, `assets.go`, `media.go`, `wailslog.go`: window specs, popup sizing, tray
  icon, embedded assets, the media handler, the logger Wails gets. `assetquiet.go`: the asset middleware that answers
  the webview's own `/favicon.ico` and `/wails/custom.js` probes with 204, so the console shows no 404.
- `agent*.go`: the agent panel. `agent.go` (`AgentService`, the `AgentDriver` interface that the app fills with the
  `Driver` of `paratech/desktop/internal/input/agent`), `agentpanel.go` (`AgentPanel`: Driver updates as Wails
  events, card views),
  `agentconfirm*.go` (`AgentConfirmService`: «Да» / «Нет» of confirm cards), `agentchats.go`, `agentjournal.go`,
  `agentdraft.go`, `agentmentions.go`.
- One file or prefix group per page: `library*.go`, `homework*.go`, `recording*.go`, `notes.go`, `tickets.go`,
  `turnkey.go`, `answers.go`, `settings.go`, `connections.go`, `google.go`, `calendar.go`, `today.go`, ...

## Where to add a binding
1. `<page>.go`: the use-case interface, the DTOs, a constructor (`NewTicketService(core, log)`), exported methods. Every exported
   method is callable from the page. Conventions (see `tickets.go`): JSON tags in camelCase, IDs as strings converted
   to `shared` IDs, empty lists as `[]`, an error type of fixed Russian texts with causes only logged (as a class or a sentinel's text, never the
   wrapped chain, which may carry row data: see `LibraryService.fail`), op names as
   constants (`"ui: ..."`).
2. `shell.go`: a field in `Core` and its nil check in `OpenServices`, a field in `Services` and its constructor.
3. `shell_windows.go`: `app.RegisterService(application.NewService(b.<Name>))`.
4. `paratech/desktop/internal/app/core.go`: fill the new field in the `wails.Core` literal.
5. Frontend: there is no codegen. Mirror the method names by hand in `paratech/desktop/frontend/src/bindings/<name>.ts`
   as `"paratech/desktop/internal/input/wails.<Type>.<Method>"` (see `paratech/desktop/frontend/src/bindings/runtime.ts`).
6. Tests: `<page>_test.go`; `validCore()` in `shell_test.go` gets the new field.
A new dialog: implement it on `windowsUI` in `<x>picker_windows.go` and reach it through a one-method interface
asserted on the running UI, as `pickRecordingFile` in `recordingimport.go` does, so `ui` and `fakeUI` do not grow;
hand it to its service through `Shell.dialogs` (see E2E below).

## Rules
ARCH-33, ARCH-40..44, ARCH-50 in `docs/architecture.md` (§6 explains why this package never imports
`paratech/desktop/internal/input/agent`).
ADR-0001, ADR-0007 (Wails, single instance, popup), ADR-0021 and ADR-0022 (only a click here confirms what the agent
proposed: `AgentConfirmService.Confirm`), ADR-0033 (agent parity: a new UI action needs an agent tool or a UI-only
reason), ADR-0034 (navigation).

## Tests
`go -C paratech test -race ./desktop/internal/input/wails/...` (on Windows this includes `*_windows_test.go`, which
build Wails options but never call `application.New`); from another OS
`GOOS=windows go -C paratech vet ./desktop/internal/input/wails/...`.
Fakes live in the test file of each page (`fakeSettings` in `settings_test.go`, `fakeUI` in `shell_test.go`).
`agentsummary_test.go` runs the real `agentchats` on `memChatStore` to pin that no chat binding or update returns a
chat's summary (ADR-0028); a new chat binding goes into its list.

## Gotchas
- `TestFrontendCallsBoundNames` (`settings_test.go`) pins the exported method count of the services it lists and
  checks that the bindings file names each method; a new exported method on those services must update both (TASK-237).
- Wails answers every error of a bound method with HTTP 422, which the webview console logs as a failed request: a
  normal empty state is `nil, nil` (JSON null), not an error (`TurnkeyService.WorkOfAssignment`).
- Nothing tests the `RegisterService` list: a service missing there builds and passes tests but is not callable.
- Wails logs binding arguments at Debug, so `Shell.WailsLogger` floors it at Info; never lower it (secrets arrive
  as arguments: `SetMailPassword`, `SetHubKey`, `SetGoogleClient`).
- Confirm-card targets are `values.KindProposal`, `values.KindPreview`, `values.KindAssignment`
  (`paratech/desktop/internal/domain/agent/values`) plus `agentCardScheduleSubjects`; a new one needs a branch in
  `AgentConfirmService.Card` and `Decline` and a confirm path (`Confirm`, or its own method as `ConfirmSchedule`).

## E2E
`Options.Hooks` (`shell.go`) replaces the open and save dialogs, the clipboard and `Core.Links` / `Core.Opener`; a nil
field keeps the native one. `Shell.dialogs` routes every dialog through its hook with a fixed `Dialog*` name, so the
override works on every OS and the `*picker_windows.go` stay as they are. Only `paratech/desktop/internal/app` sets the
hooks (`e2eHooks` in `app/e2e.go`; the `e2ehooks` adapter only in a build with tag `e2e`, `app/e2e_hooks_on.go`,
E2E mode only); no bound method takes or returns them
(`TestBoundServicesDoNotExposeHooks`). A new dialog gets a `Dialog*` name and a branch in `Shell.dialogs`.
ADR-0035 §6 plans packs under `paratech/e2e/specs/<pack>` (settings and connections, recording, popup and capture,
assignments, agent, layout); the directory does not exist yet. Every pack drives this package's bindings.
