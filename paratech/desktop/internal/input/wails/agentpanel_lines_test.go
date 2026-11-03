package wails

import (
	"encoding/json"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

// escLT and escGT are angle brackets as encoding/json escapes them.
const (
	escLT = "\\" + "u003c"
	escGT = "\\" + "u003e"
)

// withExtra is the JSON of a card (card) with the omitempty fields extra appended in the struct order.
func withExtra(fields, extra string) string {
	return strings.TrimSuffix(card(fields), "}") + "," + extra + "}"
}

// TestPanelShowsTheWave10Lines: the delete_assignment card and every result of an action line reach the panel with
// these exact bytes; the generic line keeps its bytes (TASK-324).
func TestPanelShowsTheWave10Lines(t *testing.T) {
	line := func(r values.LineResult) values.ActionLine { return values.ActionLine{Action: "act-1", Result: r} }
	tests := []struct {
		name string
		card values.Card
		want string
	}{
		{
			"generic line, unchanged", values.ActionLine{Action: "act-1"},
			card(`"kind":"action_line","actionId":"act-1"`),
		},
		{
			"delete_assignment", values.DeleteAssignmentCard{Assignment: mustRef(t, values.KindAssignment, "a-1")},
			card(`"kind":"delete_assignment","target":{"kind":"assignment","id":"a-1"}`),
		},
		{
			"marked done", line(values.LineResult{Kind: values.LineMarkedDone, Count: 3}),
			withExtra(`"kind":"action_line","actionId":"act-1"`,
				`"result":{"kind":"marked_done","count":3,"setting":"","value":"","excerpt":[]}`),
		},
		{
			"marked undone", line(values.LineResult{Kind: values.LineMarkedUndone, Count: 1}),
			withExtra(`"kind":"action_line","actionId":"act-1"`,
				`"result":{"kind":"marked_undone","count":1,"setting":"","value":"","excerpt":[]}`),
		},
		{
			"note text", line(values.LineResult{Kind: values.LineNoteText, Excerpt: "Импульс <b>тела</b>\nвторая строка"}),
			withExtra(`"kind":"action_line","actionId":"act-1"`,
				`"result":{"kind":"note_text","count":0,"setting":"","value":"","excerpt":["Импульс `+escLT+`b`+escGT+`тела`+escLT+`/b`+escGT+`","вторая строка"]}`),
		},
		{
			"note text without an excerpt", line(values.LineResult{Kind: values.LineNoteText}),
			withExtra(`"kind":"action_line","actionId":"act-1"`,
				`"result":{"kind":"note_text","count":0,"setting":"","value":"","excerpt":[]}`),
		},
		{
			"note upload", line(values.LineResult{Kind: values.LineNoteUpload}),
			withExtra(`"kind":"action_line","actionId":"act-1"`,
				`"result":{"kind":"note_upload","count":0,"setting":"","value":"","excerpt":[]}`),
		},
		{
			"note move", line(values.LineResult{Kind: values.LineNoteMove}),
			withExtra(`"kind":"action_line","actionId":"act-1"`,
				`"result":{"kind":"note_move","count":0,"setting":"","value":"","excerpt":[]}`),
		},
		{
			"note rename", line(values.LineResult{Kind: values.LineNoteRename}),
			withExtra(`"kind":"action_line","actionId":"act-1"`,
				`"result":{"kind":"note_rename","count":0,"setting":"","value":"","excerpt":[]}`),
		},
		{
			"note remove", line(values.LineResult{Kind: values.LineNoteRemove}),
			withExtra(`"kind":"action_line","actionId":"act-1"`,
				`"result":{"kind":"note_remove","count":0,"setting":"","value":"","excerpt":[]}`),
		},
		{
			"recording started", line(values.LineResult{Kind: values.LineRecordingStarted}),
			withExtra(`"kind":"action_line","actionId":"act-1"`,
				`"result":{"kind":"recording_started","count":0,"setting":"","value":"","excerpt":[]}`),
		},
		{
			"recording stopped offers a start",
			values.ActionLine{Action: "act-1", Offer: values.OfferStartRecording, Result: values.LineResult{Kind: values.LineRecordingStopped}},
			withExtra(`"kind":"action_line","actionId":"act-1"`,
				`"offer":{"action":"start_recording","target":null},`+
					`"result":{"kind":"recording_stopped","count":0,"setting":"","value":"","excerpt":[]}`),
		},
		{
			"setting", line(values.LineResult{Kind: values.LineSetting, Setting: values.SettingRecordingPrompts, Value: values.SettingOff}),
			withExtra(`"kind":"action_line","actionId":"act-1"`,
				`"result":{"kind":"setting","count":0,"setting":"recording_prompts","value":"off","excerpt":[]}`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := cardView(tt.card)
			if !ok {
				t.Fatal("the card was dropped")
			}
			got, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("card JSON =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// TestPanelDropsABrokenLine: a line result that breaks its rules never reaches the page, nor a delete card of
// another ID kind.
func TestPanelDropsABrokenLine(t *testing.T) {
	for name, c := range map[string]values.Card{
		"count 0":           values.ActionLine{Action: "act-1", Result: values.LineResult{Kind: values.LineMarkedDone}},
		"count over 50":     values.ActionLine{Action: "act-1", Result: values.LineResult{Kind: values.LineMarkedDone, Count: 51}},
		"unknown kind":      values.ActionLine{Action: "act-1", Result: values.LineResult{Kind: "sent_email"}},
		"unknown setting":   values.ActionLine{Action: "act-1", Result: values.LineResult{Kind: values.LineSetting, Setting: "profile", Value: "on"}},
		"foreign value":     values.ActionLine{Action: "act-1", Result: values.LineResult{Kind: values.LineSetting, Setting: values.SettingACPowerOnly, Value: "max"}},
		"4 excerpt lines":   values.ActionLine{Action: "act-1", Result: values.LineResult{Kind: values.LineNoteText, Excerpt: "1\n2\n3\n4"}},
		"long excerpt":      values.ActionLine{Action: "act-1", Result: values.LineResult{Kind: values.LineNoteText, Excerpt: strings.Repeat("ж", 121)}},
		"control in text":   values.ActionLine{Action: "act-1", Result: values.LineResult{Kind: values.LineNoteText, Excerpt: "раз\x1bдва"}},
		"excerpt elsewhere": values.ActionLine{Action: "act-1", Result: values.LineResult{Kind: values.LineNoteMove, Excerpt: "раз"}},
		"delete of a note":  values.DeleteAssignmentCard{Assignment: mustRef(t, values.KindNote, "n-1")},
	} {
		if _, ok := cardView(c); ok {
			t.Errorf("%s: the card reached the page", name)
		}
	}

	panel := NewAgentPanel(discard())
	em := &fakeEmitter{}
	panel.Attach(em)
	panel.Publish(AgentUpdate{Thread: "th1", Turn: 1, Status: "EXECUTING", Cards: []values.Card{
		values.ActionLine{Action: "act-1", Result: values.LineResult{Kind: values.LineMarkedDone, Count: 51}},
		values.ActionLine{Action: "act-2", Result: values.LineResult{Kind: values.LineMarkedDone, Count: 2}},
	}})
	if len(em.events) != 1 {
		t.Fatalf("events = %+v", em.events)
	}
	view := em.events[0].data[0].(AgentUpdateView)
	if len(view.Cards) != 1 || view.Cards[0].ActionID != "act-2" || view.Cards[0].Result == nil || view.Cards[0].Result.Count != 2 {
		t.Fatalf("cards = %+v", view.Cards)
	}
}
