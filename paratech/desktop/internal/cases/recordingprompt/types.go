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
	Apps []values.AudioApp // playing apps first, then by title (values.SortAudioApps)
	// Selected is the source to preselect: the remembered app when it is open, else the whole computer.
	Selected values.Source
	// Remembered is the app recorded last time, even when it is not open now, so the picker can select it as soon as
	// it appears; empty for the whole computer.
	Remembered shared.AppName
}

func viewOf(p *prompt.Prompt) View {
	return View{ID: p.ID(), Kind: p.Kind(), Lesson: p.Lesson(), State: p.State(), Problem: p.Problem()}
}
