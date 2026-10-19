package recordingprompt

import (
	"context"
	"testing"

	"paratech/desktop/internal/domain/recording/values"
)

func ownerAdvice(t *testing.T) values.SourceAdvice {
	t.Helper()
	return values.SourceAdvice{
		Silent: "browser.exe", SilentName: "Яндекс Браузер", Suggest: mustSource(t, "comet.exe"), SuggestName: "Comet",
	}
}

// TASK-082: the silent-source advice shows the popup (without focus, as the prompt does) and hides it when withdrawn.
func TestSourceAdviceShowsThePopup(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	if err := e.svc.SourceAdviceChanged(ctx, "rec-1", ownerAdvice(t)); err != nil {
		t.Fatal(err)
	}
	got, ok := e.svc.Advice()
	if !ok || got.Recording != "rec-1" || got.Advice != ownerAdvice(t) || !e.popup.Visible() || e.popup.Shows() != 1 {
		t.Fatalf("Advice = %+v, %v; popup visible %v, shows %d", got, ok, e.popup.Visible(), e.popup.Shows())
	}

	// A withdrawal for another recording changes nothing.
	if err := e.svc.SourceAdviceChanged(ctx, "rec-2", values.SourceAdvice{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.svc.Advice(); !ok || !e.popup.Visible() {
		t.Fatal("the advice of rec-1 was withdrawn by rec-2")
	}

	if err := e.svc.SourceAdviceChanged(ctx, "rec-1", values.SourceAdvice{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.svc.Advice(); ok || e.popup.Visible() {
		t.Fatal("the withdrawn advice is still shown")
	}
}

// A prompt on offer keeps the popup when the advice goes.
func TestSourceAdviceKeepsAnOfferedPrompt(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.offer("lesson-1")
	if err := e.svc.SourceAdviceChanged(ctx, "rec-1", ownerAdvice(t)); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SourceAdviceChanged(ctx, "rec-1", values.SourceAdvice{}); err != nil {
		t.Fatal(err)
	}
	if !e.popup.Visible() {
		t.Fatal("the popup of the offered prompt was hidden")
	}
}
