package recordingprompt

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/recording/prompt"
)

// DefaultPollInterval is how often Run looks at the current prompt: a snoozed prompt comes back and an expired one
// hides at most this late.
const DefaultPollInterval = 5 * time.Second

// Deps are the ports the Service needs. All are required except Log.
type Deps struct {
	Settings   SettingsReader
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
	ID        prompt.ID
	Kind      prompt.Kind
	Lesson    prompt.Lesson // zero for an ad-hoc prompt; Title is untrusted text
	State     prompt.State
	Problem   prompt.Problem // why the last start failed
	CanSnooze bool
}

func viewOf(p *prompt.Prompt, now time.Time) View {
	return View{
		ID: p.ID(), Kind: p.Kind(), Lesson: p.Lesson(), State: p.State(), Problem: p.Problem(), CanSnooze: p.CanSnooze(now),
	}
}
