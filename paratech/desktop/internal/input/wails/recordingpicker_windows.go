//go:build windows

package wails

// Texts of the open dialog of an old recording (TASK-161).
const (
	recordingDialogTitle   = "Выбор записи лекции"
	recordingFilterName    = "Аудио (*.wav, *.flac, *.mp3, *.ogg)"
	recordingFilterPattern = "*.wav;*.flac;*.mp3;*.ogg"
)

// pickRecordingFile shows the open dialog of an audio file of a lecture. The filter is a convenience only: the format
// is told from the content (recording.DetectAudio), never from the name.
func (u *windowsUI) pickRecordingFile() (string, error) {
	return u.pickFile(recordingDialogTitle, recordingFilterName, recordingFilterPattern)
}
