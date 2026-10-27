//go:build windows

package wails

// Texts of the open dialog of the notes; the filter is the library's.
const noteDialogTitle = "Выбор файлов конспекта"

// pickNoteFiles shows the open dialog of the files of notes, modal to the main window: several files when multiple
// (OpenFileDialogStruct.PromptForMultipleSelection, wails v3.0.0-beta.29), else one. The filter is a convenience only:
// the type is told from the content. None means the user cancelled.
func (u *windowsUI) pickNoteFiles(multiple bool) ([]string, error) {
	if !multiple {
		path, err := u.pickFile(noteDialogTitle, materialFilterName, materialFilterPattern)
		if err != nil || path == "" {
			return nil, err
		}
		return []string{path}, nil
	}
	paths, err := u.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		SetTitle(noteDialogTitle).
		AddFilter(materialFilterName, materialFilterPattern).
		AttachToWindow(u.main).
		PromptForMultipleSelection()
	if err != nil && err.Error() == dialogCancelled {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return pickedPaths(paths), nil
}
