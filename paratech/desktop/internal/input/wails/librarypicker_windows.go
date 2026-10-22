//go:build windows

package wails

// Texts of the open dialog of the library.
const (
	materialDialogTitle   = "Выбор файла для предмета"
	materialFilterName    = "Документы (*.docx, *.pptx, *.pdf, *.txt)"
	materialFilterPattern = "*.docx;*.pptx;*.pdf;*.txt"
)

// pickMaterialFile shows the open dialog of a file of the library. The filter is a convenience only: the type is told
// from the content (material.DetectType), never from the name.
func (u *windowsUI) pickMaterialFile() (string, error) {
	return u.pickFile(materialDialogTitle, materialFilterName, materialFilterPattern)
}
