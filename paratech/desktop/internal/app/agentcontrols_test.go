package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/recording/prompt"
	recvalues "paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// The control tools of the agent run on the use cases of the pages (TASK-322): the settings of the settings page, the
// running recording of the recordings page, «Обновить» of the connections page. Headless there is no recording
// prompt, so a start is refused as unavailable; nothing is recorded and no Hub is called without an address.
func TestAgentControlsWiring(t *testing.T) {
	ctx := context.Background()
	a := newTestApp(t, filepath.Join(t.TempDir(), "data"))
	if _, err := a.openCore(ctx); err != nil {
		t.Fatal(err)
	}
	prefs, ok := a.core.ui.Settings.(*preferences.Service)
	if !ok {
		t.Fatalf("settings page use case = %T", a.core.ui.Settings)
	}
	ctl := agentControls(a.core, agentReads{settings: prefs})

	if _, err := ctl.Settings.SetRecognitionLoad(ctx, shared.LoadMax); err != nil {
		t.Fatal(err)
	}
	if got, err := prefs.Get(ctx); err != nil || got.RecognitionLoad != shared.LoadMax {
		t.Fatalf("settings page after the agent's change = %+v, %v", got, err)
	}

	if ctl.Session == nil {
		t.Fatal("the recording session is not wired")
	}
	if _, running := ctl.Session.Active(); running {
		t.Fatal("a recording runs in a fresh core")
	}
	m, err := ctl.Starts.StartManual(ctx, recvalues.WholeComputer())
	if !errors.Is(err, recordingprompt.ErrStartFailed) || m.Problem != prompt.ProblemUnavailable {
		t.Fatalf("headless start = %+v, %v; want refused as unavailable", m, err)
	}
	if src, err := ctl.Starts.Sources(ctx); err == nil || src.Selected != recvalues.WholeComputer() {
		t.Fatalf("headless sources = %+v, %v", src, err)
	}

	r, err := ctl.Quota.RefreshQuota(ctx)
	if err == nil || (r.Outcome() != values.QuotaNotConfigured && r.Outcome() != values.QuotaNoKey) {
		t.Fatalf("quota without a Hub = %v, %v; want not configured", r.Outcome(), err)
	}
}

// Before the pages are wired the controls refuse instead of failing on a nil use case.
func TestAgentControlsNotWired(t *testing.T) {
	ctx := context.Background()
	ctl := agentControls(&core{}, agentReads{})
	if ctl.Settings != nil || ctl.Session != nil {
		t.Fatalf("controls = %+v, want no settings and no session", ctl)
	}
	if _, err := ctl.Quota.RefreshQuota(ctx); !errors.Is(err, errNotWired) {
		t.Fatalf("quota = %v", err)
	}
	if _, err := ctl.Starts.Sources(ctx); !errors.Is(err, errNotWired) {
		t.Fatalf("sources = %v", err)
	}
}
