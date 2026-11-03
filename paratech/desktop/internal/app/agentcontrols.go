package app

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/recording/prompt"
	recvalues "paratech/desktop/internal/domain/recording/values"
	agentloop "paratech/desktop/internal/input/agent"
)

// errNotWired: the use case of an agent control is not built (headless has no recording prompt).
var errNotWired = errors.New("app: agent: not wired")

// agentControls are the use cases of get_settings, set_setting, quota_status and recording (TASK-322): the same ones
// as the settings page, the connections page and «Начать запись» / «Остановить» of the recordings page. The
// recording prompt and the connections are built after the agent, so they are read at call time.
func agentControls(c *core, reads agentReads) agentloop.Controls {
	out := agentloop.Controls{Quota: agentQuota{c}, Starts: agentRecordingStarts{c}}
	if reads.settings != nil {
		out.Settings = reads.settings
	}
	if c.recordings != nil {
		out.Session = c.recordings
	}
	return out
}

// agentQuota is «Обновить» of the quota on the connections page, once it is wired.
type agentQuota struct{ c *core }

// RefreshQuota forwards to the connections use case.
func (q agentQuota) RefreshQuota(ctx context.Context) (values.QuotaReading, error) {
	if q.c.ui.Connections == nil {
		return values.QuotaReading{}, fmt.Errorf("%w: connections", errNotWired)
	}
	return q.c.ui.Connections.RefreshQuota(ctx)
}

// agentRecordingStarts is the picker's preselection and the start of «Начать запись», once the recording prompt is
// wired (UI mode); headless a start is refused as unavailable.
type agentRecordingStarts struct{ c *core }

// Sources forwards to the recording prompt.
func (s agentRecordingStarts) Sources(ctx context.Context) (recordingprompt.Sources, error) {
	if s.c.prompts == nil {
		return recordingprompt.Sources{Selected: recvalues.WholeComputer()}, fmt.Errorf("%w: recording prompt", errNotWired)
	}
	return s.c.prompts.Sources(ctx)
}

// StartManual forwards to the recording prompt.
func (s agentRecordingStarts) StartManual(ctx context.Context, source recvalues.Source) (recordingprompt.Manual, error) {
	if s.c.prompts == nil {
		return recordingprompt.Manual{Problem: prompt.ProblemUnavailable},
			fmt.Errorf("%w: %w: recording prompt", recordingprompt.ErrStartFailed, errNotWired)
	}
	return s.c.prompts.StartManual(ctx, source)
}
