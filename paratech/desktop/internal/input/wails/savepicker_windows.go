//go:build windows

package wails

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Texts of the save dialog of the support bundle.
const (
	bundleDialogTitle   = "Сохранение пакета для поддержки"
	bundleFilterName    = "Архив ZIP (*.zip)"
	bundleFilterPattern = "*.zip"
)

// pickBundleFile shows the native save dialog, modal to the main window, with name proposed (wails v3.0.0-beta.29:
// the save dialog has no SetTitle, so the title goes through the options; Windows asks before overwriting). "" means
// the user cancelled.
func (u *windowsUI) pickBundleFile(name string) (string, error) {
	path, err := u.app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:                bundleDialogTitle,
		Filename:             name,
		Filters:              []application.FileFilter{{DisplayName: bundleFilterName, Pattern: bundleFilterPattern}},
		CanCreateDirectories: true,
		Window:               u.main,
	}).PromptForSingleSelection()
	switch {
	case err != nil && err.Error() == dialogCancelled:
		return "", nil
	case err != nil:
		return "", err
	case path == "" || path == ".": // filepath.Clean of an empty result
		return "", nil
	}
	return path, nil
}

// Texts of the save dialogs of a transcript and of a summary.
const (
	transcriptDialogTitle = "Сохранение расшифровки"
	summaryDialogTitle    = "Сохранение конспекта"
	transcriptFilterText  = "Текст (*.txt)"
	transcriptFilterMD    = "Markdown (*.md)"
)

// pickTranscriptFile shows the save dialog of a transcript (pickTextFile).
func (u *windowsUI) pickTranscriptFile(name, format string) (string, error) {
	return u.pickTextFile(transcriptDialogTitle, name, format)
}

// pickSummaryFile shows the save dialog of a summary (pickTextFile).
func (u *windowsUI) pickSummaryFile(name, format string) (string, error) {
	return u.pickTextFile(summaryDialogTitle, name, format)
}

// pickTextFile shows the native save dialog, modal to the main window, with name proposed and the one filter of
// format, whose extension the dialog adds to a typed name (Wails takes the default extension from the first filter).
// "" means the user cancelled.
func (u *windowsUI) pickTextFile(title, name, format string) (string, error) {
	filter := application.FileFilter{DisplayName: transcriptFilterText, Pattern: "*.txt"}
	if format == "md" {
		filter = application.FileFilter{DisplayName: transcriptFilterMD, Pattern: "*.md"}
	}
	path, err := u.app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:                title,
		Filename:             name,
		Filters:              []application.FileFilter{filter},
		CanCreateDirectories: true,
		Window:               u.main,
	}).PromptForSingleSelection()
	switch {
	case err != nil && err.Error() == dialogCancelled:
		return "", nil
	case err != nil:
		return "", err
	case path == "" || path == ".": // filepath.Clean of an empty result
		return "", nil
	}
	return path, nil
}

// errClipboard means Windows refused the clipboard (another program holds it open).
var errClipboard = errors.New("wails: the clipboard is not available")

// setClipboardText puts text on the clipboard natively (Wails runs it on the main thread).
func (u *windowsUI) setClipboardText(text string) error {
	if !u.app.Clipboard.SetText(text) {
		return errClipboard
	}
	return nil
}
