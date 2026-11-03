package agent

import (
	"slices"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/values"
)

// offerUnavailable are the buttons this desktop cannot show yet: the call link of a lesson has no binding until
// TASK-261, and the agent's buttons for it come with TASK-328.
var offerUnavailable = []values.OfferAction{values.OfferOpenCallLink, values.OfferCopyCallLink}

// cardTool runs a UI call the Driver gets (class UI; show_plan, ask_questions, reply and cannot_do are handled by the
// step use case): offer_button. Nothing runs and nothing is journaled.
func cardTool(c agentturn.Call) ran {
	if c.Call.Tool() == values.ToolOfferButton {
		return offerButton(c)
	}
	return ran{out: agentturn.Outcome{Result: values.RefusalResult(c.Call.ID(), values.RefuseUnavailable)}}
}

// offerButton runs offer_button (ADR-0033 §3): the card is the page's own button for the action, built from the
// template of the action and the one ID it needs, which passed the provenance check of its kind in thread.Admit (an
// ID this chat has not seen is unknown_id there). The model gives no URL, path, address or label: the catalog has no
// such argument and this checks again that the call names exactly the action and its ID. The click on the card is
// the student's and calls the page's binding; the planner gets only that the button is shown.
func offerButton(c agentturn.Call) ran {
	id := c.Call.ID()
	refuse := func(why values.Refusal) ran {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, why)}}
	}
	args := c.Call.Args()
	act, _ := args.Enum(values.ArgAction)
	action := values.OfferAction(act)
	arg, kind, ok := values.OfferTarget(action)
	if !ok {
		return refuse(values.RefuseInvalidArgs)
	}
	want := []string{values.ArgAction}
	if arg != "" {
		want = append(want, arg)
	}
	for _, name := range args.Names() {
		if !slices.Contains(want, name) {
			return refuse(values.RefuseInvalidArgs)
		}
	}
	if slices.Contains(offerUnavailable, action) {
		return refuse(values.RefuseUnavailable)
	}

	card := values.OfferButtonCard{Action: action}
	if arg != "" {
		target, ok := args.ID(arg)
		if !ok || target.Kind() != kind {
			return refuse(values.RefuseInvalidArgs)
		}
		card.Target = target
	}
	if card.Validate() != nil {
		return refuse(values.RefuseInvalidArgs)
	}
	return ran{out: agentturn.Outcome{Result: values.ShownResult(id)}, cards: []values.Card{card}}
}
