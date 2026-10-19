package recordingprompt

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// DefaultPollInterval is how often Run looks at the current prompt: an expired one hides at most this late.
const DefaultPollInterval = 5 * time.Second

// Deps are the ports the Service needs. All are required except Log.
type Deps struct {
	Settings   SettingsStore
	Apps       AudioApps
	Popup      Popup
	Recordings Recordings
	Lessons    Lessons
	Clock      Clock
	Sleeper    Sleeper
	Log        *slog.Logger // optional: nil discards
}

// Config tunes the Service.
type Config struct {
	PollInterval time.Duration // positive
}

// DefaultConfig returns a poll every DefaultPollInterval.
func DefaultConfig() Config { return Config{PollInterval: DefaultPollInterval} }

// View is a prompt as the popup shows it.
type View struct {
	ID      prompt.ID
	Kind    prompt.Kind
	Lesson  prompt.Lesson // zero for an ad-hoc prompt; Title is untrusted text
	State   prompt.State
	Problem prompt.Problem // why the last start failed
}

// Sources is what the source picker offers (TASK-059).
type Sources struct {
	Apps []values.AudioApp // sounding apps first, then playing ones, then by title (values.SortAudioApps)
	// Selected is the source to preselect (values.Preselect): the remembered app when it sounds or no single other app
	// does, the one other app that sounds (AutoSelected), else the whole computer.
	Selected values.Source
	// AutoSelected reports that Selected was chosen because it plays while the remembered app does not (TASK-082): the
	// picker says «выбрано: звук идёт отсюда».
	AutoSelected bool
	// Remembered is the app recorded last time, even when it is not open now, so the picker can select it as soon as
	// it appears; empty for the whole computer.
	Remembered shared.AppName
}

// Manual is the result of a start from the main window (TASK-081).
type Manual struct {
	Recording shared.RecordingID // set once the recording runs
	Lesson    shared.LessonID    // the lesson in progress the recording is linked to; empty without one
	Problem   prompt.Problem     // why it did not start; prompt.ProblemNone once it runs
}

// Advice is the silent-source advice of the running recording as the popup shows it (TASK-082).
type Advice struct {
	Recording shared.RecordingID
	Advice    values.SourceAdvice
}

func viewOf(p *prompt.Prompt) View {
	return View{ID: p.ID(), Kind: p.Kind(), Lesson: p.Lesson(), State: p.State(), Problem: p.Problem()}
}
