//go:build windows

package wails

// Texts of the open dialog of the notes: the library's documents and photos of handwriting (TASK-165).
const (
	noteDialogTitle   = "Выбор файлов конспекта"
	noteFilterName    = "Конспекты (*.docx, *.pptx, *.pdf, *.txt, *.jpg, *.jpeg, *.png)"
	noteFilterPattern = materialFilterPattern + ";*.jpg;*.jpeg;*.png"
)

// pickNoteFiles shows the open dialog of the files of notes, modal to the main window: several files when multiple
// (OpenFileDialogStruct.PromptForMultipleSelection, wails v3.0.0-beta.29), else one. The filter is a convenience only:
// the type is told from the content. None means the user cancelled.
func (u *windowsUI) pickNoteFiles(multiple bool) ([]string, error) {
	if !multiple {
		path, err := u.pickFile(noteDialogTitle, noteFilterName, noteFilterPattern)
		if err != nil || path == "" {
			return nil, err
		}
		return []string{path}, nil
	}
	paths, err := u.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		SetTitle(noteDialogTitle).
		AddFilter(noteFilterName, noteFilterPattern).
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
