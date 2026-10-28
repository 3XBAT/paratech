package values

// MaxStepMentions bounds the mentions one step sends (gateway.proto AgentScope.mentions): at most MaxMentions of the
// current request and the rest from earlier requests of the history window.
const MaxStepMentions = 16

// StepMention is a mention as a step sends it to the Hub (ADR-0028 «References, not copies»): the typed ID of the
// object and whether the current request mentions it (false: an earlier request of the window). It never holds the
// label, which is display text only (ADR-0022).
type StepMention struct {
	Ref     Ref
	Current bool
}

// StepMentionRefs returns the IDs of the step mentions.
func StepMentionRefs(ms []StepMention) []Ref {
	out := make([]Ref, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Ref)
	}
	return out
}
