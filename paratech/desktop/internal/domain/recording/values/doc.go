// Package values holds value objects shared by the aggregates and use cases of the recording context: the recording
// Source (the whole computer or one app) and AudioApp, a candidate in the source picker, with the picker's ordering
// and preselection rules (TASK-059); the capture diagnostics (TASK-058): CaptureMode, CaptureAttempt, CaptureSwitch
// with the fixed problem note of a fallback, StreamFormat, WindowsVersion and CaptureDiagnostics, and AppBuild, the build
// of this executable in the support bundle; and the "Check sound" level check (TASK-075): SoundLevel, the threshold of
// sound heard, LevelCheck and its verdict (SoundCause); and the protection from empty recordings (TASK-082): readable app
// names (DisplayName), SourceWatch with its SourceAdvice (the chosen app is silent while another one sounds) and
// AssembledTrack, which tells a silent final track.
package values
