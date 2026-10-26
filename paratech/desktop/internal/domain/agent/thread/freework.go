package thread

import (
	"fmt"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
)

// FreeWorkCall finds the call a free-work card names (TASK-188) in entries, the stored history of the card's own
// thread: exactly one sealed call callID of request turn, parsed again with ParseCall, a work on a free topic, and
// answered with the free-work result (values.FreeWorkResult), so only a call that waited on the card is ever run by
// the student's «Сделать». Anything else (another thread's history, a made-up ID, a call that ran or was refused) is
// ErrUnknownCall. The Admission is the decision on that call: the card's «Сделать» is the student's consent, so the call
// runs with the class and the paid flag of its policy entry, outside any plan (PlanStep -1).
func FreeWorkCall(entries []values.HistoryEntry, turn int, callID string) (values.Call, Admission, error) {
	var (
		raws    []values.RawCall
		results []values.ToolResult
	)
	for _, e := range entries {
		if e.Turn != turn {
			continue
		}
		if e.Entry.Calls != nil {
			for _, c := range e.Entry.Calls.Calls {
				if c.CallID == callID {
					raws = append(raws, c)
				}
			}
		}
		for _, r := range e.Entry.Results {
			if r.CallID == callID {
				results = append(results, r)
			}
		}
	}
	unknown := func(what string) (values.Call, Admission, error) {
		return values.Call{}, Admission{}, fmt.Errorf("%w: free work %d/%s: %s", ErrUnknownCall, turn, callID, what)
	}
	if len(raws) != 1 || len(results) != 1 {
		return unknown("not exactly one stored call and result")
	}
	if !values.IsFreeWorkResult(results[0]) {
		return unknown("the call did not wait on the free-work card")
	}
	call, err := values.ParseCall(raws[0].CallID, raws[0].Tool, raws[0].ArgumentsJSON)
	if err != nil {
		return unknown("the stored call does not parse")
	}
	if !actionpolicy.FreeTopic(call) {
		return unknown("not a work on a free topic")
	}
	rule, err := actionpolicy.Of(call.Tool())
	if err != nil {
		return unknown("the stored call has no policy")
	}
	return call, Admission{Class: rule.Class, Paid: rule.Paid, PlanStep: -1}, nil
}
