// Package wails is the Windows UI shell of the desktop core: the main window, the tray icon and the recording
// popup (PRD section 6.1, AC-01), built on Wails v3 (ADR-0001, ADR-0007).
//
// Wails code lives only in *_windows.go files (ARCH-33); on other systems Shell.Run returns ErrUnsupported, so the
// module builds and tests without CGO or a WebView. The pages call use cases only through the bound services
// (settings.go; ARCH-40..44), which the shell registers after the single-instance check.
//
// The popup shows the current recording prompt (prompt.go, PromptService): Shell.ShowPopup/HidePopup are the Popup port
// of cases/recordingprompt, ShowPopup emits PromptShownEvent for the page, and the tray's "Show recording popup" asks
// the core for an ad-hoc prompt.
//
// The schedule page imports a file through ScheduleImportService (schedule.go): the shell shows the open dialog
// (schedulepicker_windows.go), the binding reads the file and keeps it until the commit, so the page never sends a path.
//
// Shell.Notify is the notifier of finished processing (PRD section 15): Windows toasts with fixed texts (notices.go)
// through the Wails notifications service, wrapped so that a failed setup never stops the app (toasts_windows.go).
package wails
