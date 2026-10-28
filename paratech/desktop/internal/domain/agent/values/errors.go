package values

import "errors"

// Errors of the agent values. Parse errors of a tool call become an is_error result for the planner; their texts
// carry the argument name and the rule, never the rejected value.
var (
	// ErrUnknownTool means the tool is not in the catalog of this desktop version.
	ErrUnknownTool = errors.New("unknown tool")

	// ErrInvalidArguments means the arguments of a call break the catalog: not a JSON object, an unknown, duplicate
	// or missing argument, a wrong type, an enum value outside the list or a broken one-of pair.
	ErrInvalidArguments = errors.New("invalid tool arguments")

	// ErrInvalidRef is returned for a malformed ID or an unknown ID kind.
	ErrInvalidRef = errors.New("invalid id")

	// ErrInvalidDate is returned for a value that is not a calendar date YYYY-MM-DD.
	ErrInvalidDate = errors.New("invalid date")

	// ErrInvalidTime is returned for a value that is not a time of day HH:MM, 24 h (catalog v6).
	ErrInvalidTime = errors.New("invalid time of day")

	// ErrInvalidText is returned for a card text, label or name that breaks its length or character rules.
	ErrInvalidText = errors.New("invalid text")

	// ErrInvalidCard is returned for a card that breaks its invariants.
	ErrInvalidCard = errors.New("invalid card")

	// ErrInvalidScope is returned for a scope or base that breaks its invariants.
	ErrInvalidScope = errors.New("invalid scope")

	// ErrInvalidMention is returned for a mention of a wrong kind, a bad label or too many mentions in a request.
	ErrInvalidMention = errors.New("invalid mention")

	// ErrInvalidSummary is returned for a history summary that breaks its range, length or character rules.
	ErrInvalidSummary = errors.New("invalid history summary")

	// ErrInvalidResult is returned for a tool result or a history entry that breaks its limits.
	ErrInvalidResult = errors.New("invalid tool result")

	// ErrTurnGone means the Hub no longer keeps the model turn of the last echoed calls (ADR-0023), so the request
	// cannot continue; it starts again from the student's text. AgentModel errors wrap it.
	ErrTurnGone = errors.New("the hub no longer keeps the model turn")
)
