package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the settings repository, used in its error messages.
const (
	opLoadSettings = "sqlite: load settings"
	opSaveSettings = "sqlite: save settings"
)

// SettingsRepository stores the settings singleton (the modelcatalog.SettingsRepository port; its Load also serves the
// SettingsReader ports of recordingsession, jobrunner and transcribe, and the mail and Hub setup the app reads for the
// smtp and hubclient adapters; with Save it is the SettingsStore of recordingprompt, which remembers the source). No column holds a secret (hard rule 4). Defaults and validation stay in
// settings.Settings: the repository only converts between the row and the aggregate.
type SettingsRepository struct{ db *DB }

// NewSettingsRepository returns the settings repository of db.
func NewSettingsRepository(db *DB) *SettingsRepository { return &SettingsRepository{db: db} }

// Load returns the stored settings, or the defaults with version 0 while no row exists (ADR-0004(d)).
func (r *SettingsRepository) Load(ctx context.Context) (*settings.Settings, error) {
	row, err := r.db.Queries(ctx).GetSettings(ctx)
	if err != nil {
		if err = MapError(ctx, err); errors.Is(err, shared.ErrNotFound) {
			return settings.New(), nil
		}
		return nil, fmt.Errorf("%s: %w", opLoadSettings, err)
	}

	s, err := settingsFromRow(row)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opLoadSettings, err)
	}
	return s, nil
}

// Save writes the settings: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). A row
// created meanwhile by another writer and a stale version both yield shared.ErrVersionConflict; a default model that
// is not installed yields shared.ErrConflict (foreign key).
func (r *SettingsRepository) Save(ctx context.Context, s *settings.Settings) error {
	if s.Version() < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveSettings, s.Version())
	}

	next := s.Version() + 1
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if s.Version() == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertSettings(ctx, settingsInsertParams(s))))
		}
		n, err := q.UpdateSettings(ctx, settingsUpdateParams(s))
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s: %w", opSaveSettings, err)
	}

	s.Persisted(next)
	return nil
}

func settingsFromRow(row sqlcgen.Setting) (*settings.Settings, error) {
	return settings.Restore(settings.State{
		Version:                 row.Version,
		RecordingPromptsEnabled: row.RecordingPromptsEnabled != 0,
		ProcessOnlyOnACPower:    row.ProcessOnlyOnAcPower != 0,
		DefaultSttModel:         shared.SttModelID(row.DefaultSttModelID.String),
		HomeworkRecipient:       row.HomeworkRecipient,
		SMTPHost:                row.SmtpHost,
		SMTPPort:                int(row.SmtpPort),
		SMTPSecurity:            settings.MailSecurity(row.SmtpSecurity),
		SMTPLogin:               row.SmtpUsername,
		HubURL:                  row.HubUrl,
		RecordingSource:         shared.AppName(row.RecordingSourceApp),
		UpdatedAt:               settingsFromMs(row.UpdatedAt),
	})
}

func settingsInsertParams(s *settings.Settings) sqlcgen.InsertSettingsParams {
	return sqlcgen.InsertSettingsParams{
		RecordingPromptsEnabled: settingsBool(s.RecordingPromptsEnabled()),
		ProcessOnlyOnAcPower:    settingsBool(s.ProcessOnlyOnACPower()),
		DefaultSttModelID:       settingsModel(s.DefaultSttModel()),
		HomeworkRecipient:       s.HomeworkRecipient(),
		SmtpHost:                s.SMTPHost(),
		SmtpPort:                int64(s.SMTPPort()),
		SmtpSecurity:            string(s.SMTPSecurity()),
		SmtpUsername:            s.SMTPLogin(),
		HubUrl:                  s.HubURL(),
		RecordingSourceApp:      s.RecordingSource().String(),
		UpdatedAt:               settingsMs(s.UpdatedAt()),
	}
}

func settingsUpdateParams(s *settings.Settings) sqlcgen.UpdateSettingsParams {
	p := settingsInsertParams(s)
	return sqlcgen.UpdateSettingsParams{
		RecordingPromptsEnabled: p.RecordingPromptsEnabled,
		ProcessOnlyOnAcPower:    p.ProcessOnlyOnAcPower,
		DefaultSttModelID:       p.DefaultSttModelID,
		HomeworkRecipient:       p.HomeworkRecipient,
		SmtpHost:                p.SmtpHost,
		SmtpPort:                p.SmtpPort,
		SmtpSecurity:            p.SmtpSecurity,
		SmtpUsername:            p.SmtpUsername,
		HubUrl:                  p.HubUrl,
		RecordingSourceApp:      p.RecordingSourceApp,
		UpdatedAt:               p.UpdatedAt,
		ExpectedVersion:         s.Version(),
	}
}

func settingsBool(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func settingsModel(id shared.SttModelID) sql.NullString {
	return sql.NullString{String: string(id), Valid: id != ""}
}

// settingsMs stores "never changed" (zero time) as 0, because updated_at is NOT NULL.
func settingsMs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func settingsFromMs(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}
