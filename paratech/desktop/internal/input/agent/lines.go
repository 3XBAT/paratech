package agent

import "paratech/desktop/internal/domain/agent/values"

// lineResult is what the action line of eff words. A result that breaks its rules (a count beyond the limit) falls
// back to the generic line, so the line and its «Отменить» are still shown.
func (eff effect) lineResult() values.LineResult {
	if eff.line.Validate() != nil {
		return values.LineResult{}
	}
	return eff.line
}

// markedLine is the line of mark_assignments_done that changed n assignments to done (or back to work).
func markedLine(done bool, n int) values.LineResult {
	if done {
		return values.LineResult{Kind: values.LineMarkedDone, Count: n}
	}
	return values.LineResult{Kind: values.LineMarkedUndone, Count: n}
}
