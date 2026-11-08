package agent

import (
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
)

// The cards the student answers or undoes come after the agent's reply, the stop line last (TASK-400); a request
// with none of them keeps its order exactly. The tool cards come in through remember, as from the loop.
func TestUpdateDisplayOrder(t *testing.T) {
	line := values.ActionLine{Action: "act1"}
	confirm := values.ConfirmCard{Target: mustRef(t, values.KindProposal, "p1")}
	stopped := values.StoppedCard{Reason: values.StopOffline}

	tests := []struct {
		name   string
		tools  []values.Card
		status thread.Status
		own    []values.Card
		want   []values.Card
	}{
		{"reply then action line", []values.Card{line}, thread.StatusDone, []values.Card{reply("Готово")},
			[]values.Card{reply("Готово"), line}},
		{"confirm after the reply", []values.Card{materialCard(t, "m1"), confirm}, thread.StatusDone, []values.Card{reply("Готово")},
			[]values.Card{materialCard(t, "m1"), reply("Готово"), confirm}},
		{"stopped after an action line", []values.Card{line}, thread.StatusStopped, []values.Card{stopped},
			[]values.Card{line, stopped}},
		{"late cards keep their order", []values.Card{confirm, line}, thread.StatusStopped, []values.Card{reply("Итог"), stopped},
			[]values.Card{reply("Итог"), confirm, line, stopped}},
		{"no late cards: order unchanged", []values.Card{materialCard(t, "m1"), materialCard(t, "m2")}, thread.StatusDone,
			[]values.Card{reply("Готово")}, []values.Card{materialCard(t, "m1"), materialCard(t, "m2"), reply("Готово")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _ := newDriver(t, newReads(), &scriptedTurns{})
			d.remember(stepOn(th1, 1, thread.StatusExecuting, nil), tt.tools)
			u := d.update(t.Context(), stepOn(th1, 1, tt.status, nil, tt.own...), ProblemNone)
			assertCards(t, tt.name, u.Cards, tt.want...)
		})
	}
}

func TestDisplayOrder(t *testing.T) {
	schedule := values.ScheduleSubjectsCard{}
	del := values.DeleteAssignmentCard{}
	tests := []struct {
		name string
		in   []values.Card
		want []values.Card
	}{
		{"empty", nil, nil},
		{"late kinds move after the rest", []values.Card{schedule, reply("a"), del, question(t)},
			[]values.Card{reply("a"), question(t), schedule, del}},
		{"stopped last", []values.Card{values.StoppedCard{Reason: values.StopFailed}, del, reply("a")},
			[]values.Card{reply("a"), del, values.StoppedCard{Reason: values.StopFailed}}},
	}
	for _, tt := range tests {
		assertCards(t, tt.name, displayOrder(tt.in), tt.want...)
	}
}
