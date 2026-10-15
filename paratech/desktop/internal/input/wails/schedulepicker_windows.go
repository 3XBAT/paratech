//go:build windows

package wails

// Texts of the open dialogs: the schedule import and the model import.
const (
	scheduleDialogTitle   = "Выбор файла расписания"
	scheduleFilterName    = "Расписание (*.ics, *.csv, *.xlsx)"
	scheduleFilterPattern = "*.ics;*.csv;*.xlsx"

	modelDialogTitle   = "Выбор файла модели"
	modelFilterName    = "Модель whisper (*.bin)"
	modelFilterPattern = "*.bin"
)

// dialogCancelled is the text of the error the Wails file dialog returns when the user cancels (wails v3.0.0-beta.29,
// cfd.ErrorCancelled in internal/go-common-file-dialog/cfd); the package is internal, so errors.Is cannot match it.
// Re-check it on a Wails upgrade (docs/refs/wails.md).
const dialogCancelled = "cancelled by user"

// pickScheduleFile shows the open dialog of the schedule import. The filter is a convenience only: ChooseFile checks
// the extension again.
func (u *windowsUI) pickScheduleFile() (string, error) {
	return u.pickFile(scheduleDialogTitle, scheduleFilterName, scheduleFilterPattern)
}

// pickModelFile shows the open dialog of the model import; the binding checks the extension again.
func (u *windowsUI) pickModelFile() (string, error) {
	return u.pickFile(modelDialogTitle, modelFilterName, modelFilterPattern)
}

// pickFile shows the native open dialog, modal to the main window. Wails shows it on the main thread and the call
// waits for the user; bound methods run on their own goroutines. "" means the user cancelled.
func (u *windowsUI) pickFile(title, filterName, filterPattern string) (string, error) {
	path, err := u.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		SetTitle(title).
		AddFilter(filterName, filterPattern).
		AttachToWindow(u.main).
		PromptForSingleSelection()
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
