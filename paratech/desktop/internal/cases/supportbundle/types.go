package supportbundle

import "log/slog"

// Deps are the ports of the Service. All are required except Captures and Log.
type Deps struct {
	Archive  ArchiveWriter
	Logs     LogFiles
	System   SystemInfo
	Settings SettingsReader
	Captures CaptureHistory // optional: nil adds no capture diagnostics
	Clock    Clock
	Log      *slog.Logger // optional: nil discards
}

// DefaultMaxLogBytes bounds the logs in a bundle: the newest 8 MiB.
const DefaultMaxLogBytes = 8 << 20

// Config tunes the Service.
type Config struct {
	MaxLogBytes int64 // 0 is DefaultMaxLogBytes
}

// The entries of the bundle as JSON. They are an allowlist: every field is named here.
type (
	about struct {
		Created string      `json:"created"`
		App     appBuild    `json:"app"`
		Windows windowsInfo `json:"windows"`
	}
	appBuild struct {
		Module       string `json:"module"`
		Version      string `json:"version"`
		Revision     string `json:"revision"`
		RevisionTime string `json:"revision_time"`
		Modified     bool   `json:"modified"`
		Go           string `json:"go"`
		OS           string `json:"os"`
	}
	windowsInfo struct {
		Name           string `json:"name"`
		DisplayVersion string `json:"display_version"`
		Build          string `json:"build"`
		Windows11      bool   `json:"windows_11"`
	}

	settingsInfo struct {
		RecordingPromptsEnabled bool   `json:"recording_prompts_enabled"`
		ProcessOnlyOnACPower    bool   `json:"process_only_on_ac_power"`
		DefaultSttModel         string `json:"default_stt_model"`
		RecordingSource         string `json:"recording_source"` // an exe name; "" is the whole computer
		HomeworkRecipient       string `json:"homework_recipient"`
		SMTPHost                string `json:"smtp_host"`
		SMTPPort                int    `json:"smtp_port"`
		SMTPSecurity            string `json:"smtp_security"`
		SMTPLogin               string `json:"smtp_login"`
		MailConfigured          bool   `json:"mail_configured"`
		HubURL                  string `json:"hub_url"`
	}

	captureInfo struct {
		Running       bool          `json:"running"`
		TargetMode    string        `json:"target_mode"`
		App           string        `json:"app"`
		Mode          string        `json:"mode"`
		Format        string        `json:"format"`
		Windows       string        `json:"windows"`
		Device        string        `json:"device"`
		Attempts      []attemptInfo `json:"attempts"`
		Switches      []switchInfo  `json:"switches"`
		Chunks        int           `json:"chunks"`
		Packets       uint64        `json:"packets"`
		SilentPackets uint64        `json:"silent_packets"`
		Frames        int64         `json:"frames"`
		Reopens       uint64        `json:"reopens"`
		Restarts      int           `json:"restarts"`
		Failure       string        `json:"failure"`
		InvalidLines  int           `json:"invalid_lines"`
		WaitingForApp bool          `json:"waiting_for_app"`
		Rebinds       int           `json:"rebinds"`
	}
	attemptInfo struct {
		Mode    string `json:"mode"`
		OK      bool   `json:"ok"`
		HResult string `json:"hresult"`
		Error   string `json:"error"`
		Device  string `json:"device"`
	}
	switchInfo struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Cause  string `json:"cause"`
		Reason string `json:"reason"`
	}
)
