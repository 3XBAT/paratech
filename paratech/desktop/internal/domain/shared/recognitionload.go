package shared

// RecognitionLoad is how hard speech recognition may load the computer (TASK-084). The settings context stores it
// and the transcription context turns it into a process policy, so it lives in the shared kernel like AppName.
type RecognitionLoad string

// Load modes, from the lightest to the heaviest.
const (
	LoadGentle RecognitionLoad = "gentle" // «Бережная»: low priority, half the cores, a hard CPU cap
	LoadNormal RecognitionLoad = "normal" // «Обычная»: low priority, the whisper-cli default threads
	LoadMax    RecognitionLoad = "max"    // «Максимальная»: no limits

	// DefaultRecognitionLoad is the mode of a fresh installation.
	DefaultRecognitionLoad = LoadGentle
)

// RecognitionLoads returns every mode, from the lightest to the heaviest.
func RecognitionLoads() []RecognitionLoad { return []RecognitionLoad{LoadGentle, LoadNormal, LoadMax} }

// Valid reports whether l is one of the modes.
func (l RecognitionLoad) Valid() bool {
	switch l {
	case LoadGentle, LoadNormal, LoadMax:
		return true
	}
	return false
}

// OrDefault returns l, or DefaultRecognitionLoad when l is not a mode (an empty or tampered stored value).
func (l RecognitionLoad) OrDefault() RecognitionLoad {
	if l.Valid() {
		return l
	}
	return DefaultRecognitionLoad
}

// String returns the mode.
func (l RecognitionLoad) String() string { return string(l) }
