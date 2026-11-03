package wails

import "paratech/desktop/internal/domain/agent/values"

// AgentOfferView is an offer_button card (ADR-0033 §3): the page's own button for Action, with the one typed ID it
// needs (null when none). The panel renders the button from its template and the click calls the page's binding for
// that action; nothing here comes from the model's text (no URL, path or label). undo_action names no action: the
// panel undoes the chat's latest action that can still be undone.
type AgentOfferView struct {
	Action string        `json:"action"`
	Target *AgentRefView `json:"target"`
}

func offerView(c values.OfferButtonCard) *AgentOfferView {
	v := &AgentOfferView{Action: string(c.Action)}
	if !c.Target.IsZero() {
		v.Target = &AgentRefView{Kind: string(c.Target.Kind()), ID: c.Target.ID()}
	}
	return v
}
