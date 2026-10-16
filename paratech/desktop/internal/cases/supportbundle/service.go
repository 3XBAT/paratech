package supportbundle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/settings/settings"
)

// Entry names of the bundle.
const (
	entryAbout    = "about.json"
	entrySettings = "settings.json"
	entryCapture  = "capture.json"
	logsDir       = "logs/"

	// profileMark replaces the user's profile directory in every text.
	profileMark = "%USERPROFILE%"
	namePrefix  = "paratech-support-"
)

// Service builds support bundles.
type Service struct {
	deps   Deps
	maxLog int64
	log    *slog.Logger
}

// New builds the Service.
func New(deps Deps, cfg Config) (*Service, error) {
	if deps.Archive == nil || deps.Logs == nil || deps.System == nil || deps.Settings == nil || deps.Clock == nil {
		return nil, fmt.Errorf("%w: the archive, logs, system, settings and clock ports are required", ErrInvalidConfig)
	}
	if cfg.MaxLogBytes <= 0 {
		cfg.MaxLogBytes = DefaultMaxLogBytes
	}
	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{deps: deps, maxLog: cfg.MaxLogBytes, log: log}, nil
}

// SuggestedName is the file name the save dialog offers: paratech-support-YYYYMMDD-HHMMSS.zip in local time.
func (s *Service) SuggestedName() string {
	return namePrefix + s.deps.Clock.Now().Format("20060102-150405") + ".zip"
}

// Create writes the bundle to path, which the user chose. Settings that cannot be read are left out (the bundle is
// for a broken app too); a failed archive leaves nothing at path.
func (s *Service) Create(ctx context.Context, path string) error {
	hide := newRedactor(s.deps.System.ProfileDir())
	now := s.deps.Clock.Now()
	entries := 0
	err := s.deps.Archive.WriteZip(ctx, path, func(add func(name string, data []byte) error) error {
		put := func(name string, data []byte) error {
			entries++
			return add(name, hide.bytes(data))
		}
		if err := putJSON(put, entryAbout, s.about(ctx, now)); err != nil {
			return err
		}
		if st, err := s.deps.Settings.Load(ctx); err != nil {
			s.log.Warn("support bundle: the settings cannot be read; left out", "err", err)
		} else if err := putJSON(put, entrySettings, settingsOf(st.SupportSummary())); err != nil {
			return err
		}
		if s.deps.Captures != nil {
			if err := putJSON(put, entryCapture, capturesOf(s.deps.Captures.RecentCaptures(ctx))); err != nil {
				return err
			}
		}
		return s.deps.Logs.EachLog(ctx, s.maxLog, func(name string, data []byte) error {
			return put(logsDir+name, data)
		})
	})
	if err != nil {
		return fmt.Errorf("supportbundle: create: %w", err)
	}
	s.log.Info("support bundle saved", "entries", entries)
	return nil
}

func putJSON(put func(string, []byte) error, name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return put(name, append(data, '\n'))
}

func (s *Service) about(ctx context.Context, now time.Time) about {
	out := about{Created: now.Format(time.RFC3339)}
	if b, ok := s.deps.System.Build(); ok && b != nil {
		out.App = appBuild{Module: b.Main.Path, Version: b.Main.Version, Go: b.GoVersion}
		var goos, goarch string
		for _, kv := range b.Settings {
			switch kv.Key {
			case "vcs.revision":
				out.App.Revision = kv.Value
			case "vcs.time":
				out.App.RevisionTime = kv.Value
			case "vcs.modified":
				out.App.Modified = kv.Value == "true"
			case "GOOS":
				goos = kv.Value
			case "GOARCH":
				goarch = kv.Value
			}
		}
		if goos != "" {
			out.App.OS = goos + "/" + goarch
		}
	}
	w := s.deps.System.Windows(ctx)
	out.Windows = windowsInfo{Name: w.Name(), DisplayVersion: w.DisplayVersion, Build: w.BuildString(), Windows11: w.IsWindows11()}
	return out
}

func settingsOf(sum settings.Summary) settingsInfo {
	return settingsInfo{
		RecordingPromptsEnabled: sum.RecordingPromptsEnabled, ProcessOnlyOnACPower: sum.ProcessOnlyOnACPower,
		DefaultSttModel: string(sum.DefaultSttModel), RecordingSource: string(sum.RecordingSource),
		HomeworkRecipient: sum.HomeworkRecipient, SMTPHost: sum.SMTPHost,
		SMTPPort: sum.SMTPPort, SMTPSecurity: string(sum.SMTPSecurity), SMTPLogin: sum.SMTPLogin,
		MailConfigured: sum.MailConfigured, HubURL: sum.HubURL,
	}
}

func capturesOf(ds []values.CaptureDiagnostics) []captureInfo {
	out := make([]captureInfo, 0, len(ds))
	for _, d := range ds {
		c := captureInfo{
			Running: d.Running, TargetMode: string(d.TargetMode), App: d.App.String(), Mode: string(d.Mode),
			Format: d.Format.String(), Windows: d.Windows.String(), Device: d.Device, Chunks: d.Chunks,
			Packets: d.Stats.Packets, SilentPackets: d.Stats.SilentPackets, Frames: d.Stats.Frames, Reopens: d.Stats.Reopens,
			Restarts: d.Restarts, Failure: d.Failure, InvalidLines: d.InvalidLines, WaitingForApp: d.WaitingForApp,
			Rebinds: d.Rebinds, Attempts: make([]attemptInfo, 0, len(d.Attempts)), Switches: make([]switchInfo, 0, len(d.Switches)),
		}
		for _, a := range d.Attempts {
			c.Attempts = append(c.Attempts, attemptInfo{Mode: string(a.Mode), OK: a.OK, HResult: a.HResult, Error: a.Error, Device: a.Device})
		}
		for _, sw := range d.Switches {
			c.Switches = append(c.Switches, switchInfo{From: string(sw.From), To: string(sw.To), Cause: string(sw.Cause), Reason: sw.Reason})
		}
		out = append(out, c)
	}
	return out
}

// redactor replaces the user's profile directory, as typed, as the log quotes it (doubled backslashes) and with
// forward slashes, ignoring case as Windows does.
type redactor struct{ needles []string }

func newRedactor(profile string) redactor {
	profile = strings.TrimRight(profile, `\/`)
	if len(profile) < 3 { // "C:" or nothing: replacing it would only garble the texts
		return redactor{}
	}
	seen := map[string]bool{}
	var r redactor
	for _, n := range []string{
		strings.ReplaceAll(profile, `\`, `\\`), profile, strings.ReplaceAll(profile, `\`, "/"),
	} {
		if !seen[n] {
			seen[n] = true
			r.needles = append(r.needles, n)
		}
	}
	return r
}

func (r redactor) bytes(b []byte) []byte {
	if len(r.needles) == 0 {
		return b
	}
	s := string(b)
	for _, n := range r.needles {
		s = replaceFold(s, n, profileMark)
	}
	return []byte(s)
}

// replaceFold replaces every occurrence of old in s, ignoring case, by repl.
func replaceFold(s, old, repl string) string {
	if old == "" || len(s) < len(old) {
		return s
	}
	first := old[0]
	var b strings.Builder
	last := 0
	for i := 0; i+len(old) <= len(s); {
		c := s[i]
		if (c == first || c|0x20 == first|0x20) && strings.EqualFold(s[i:i+len(old)], old) {
			b.WriteString(s[last:i])
			b.WriteString(repl)
			i += len(old)
			last = i
			continue
		}
		i++
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}
