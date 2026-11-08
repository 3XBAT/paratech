package agent

import (
	"reflect"
	"slices"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/values"
)

// maxShownCards bounds the tool cards kept for one request. A request runs at most MaxPlanRunSteps steps of a few
// calls each, so real requests stay far below it; it only guards memory against a broken invariant. Beyond it the
// oldest card goes.
const maxShownCards = 64

// toolCards are the cards the tools of request turn showed, in order of arrival, raw: update checks them on every
// publish.
type toolCards struct {
	turn  int
	cards []values.Card
}

// remember keeps the tool cards a step of st showed. A newer request of the thread starts empty; a step of an older
// one changes nothing. A card equal to one kept is not added again (a replayed read shows the same card).
func (d *Driver) remember(st agentturn.Step, cards []values.Card) {
	if st.Thread == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	k := d.shown[st.Thread]
	switch {
	case k == nil || st.Turn > k.turn:
		k = &toolCards{turn: st.Turn}
		d.shown[st.Thread] = k
	case st.Turn < k.turn:
		return
	}
	for _, c := range cards {
		if slices.ContainsFunc(k.cards, func(o values.Card) bool { return reflect.DeepEqual(o, c) }) {
			continue
		}
		k.cards = append(k.cards, c)
		if len(k.cards) > maxShownCards {
			k.cards = k.cards[1:]
		}
	}
}

// displayOrder orders the cards of an update for reading (owner 08.11): what the agent says first, then the cards the
// student answers or undoes after reading it (confirm, action line, schedule, delete), then the stop line as the final
// status. Each group keeps its order.
func displayOrder(cards []values.Card) []values.Card {
	var says, late, stopped []values.Card
	for _, c := range cards {
		switch c.(type) {
		case values.ConfirmCard, values.ActionLine, values.ScheduleSubjectsCard, values.DeleteAssignmentCard:
			late = append(late, c)
		case values.StoppedCard:
			stopped = append(stopped, c)
		default:
			says = append(says, c)
		}
	}
	out := make([]values.Card, 0, len(cards))
	out = append(out, says...)
	out = append(out, late...)
	return append(out, stopped...)
}

// shownCards are the tool cards of the request of step st, merged with its own cards by update. Only a request-state update gets them: one with
// no thread (a failed Ask) or no status (a refused button: the panel keeps what it shows) gets none, and so does an
// update of an older request than the one kept. A newer request drops the kept cards.
func (d *Driver) shownCards(st agentturn.Step) []values.Card {
	if st.Thread == "" || st.Status == "" {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	k := d.shown[st.Thread]
	switch {
	case k == nil || st.Turn < k.turn:
		return nil
	case st.Turn > k.turn:
		k.turn, k.cards = st.Turn, nil
		return nil
	}
	return slices.Clone(k.cards)
}
