package eventrefresh

// Policy is what a deadline change may do to the Google event.
type Policy string

// Policies.
const (
	PolicyAsk    Policy = "ask"    // the student is asked; the default
	PolicyUpdate Policy = "update" // the event follows the deadline without a question
	PolicyKeep   Policy = "keep"   // the event stays as it is
)

// Action is what the use case does after a deadline change.
type Action string

// Actions.
const (
	Nothing      Action = "nothing"       // the event is not touched
	Ask          Action = "ask"           // the student is asked «Обновить событие в Google Календаре?»
	QueueUpdate  Action = "queue_update"  // a deadline_event_updates row: the sync moves the event to the new deadline
	QueueRemoval Action = "queue_removal" // a deadline_event_removals row: the deadline is gone, so is the event
)

// Decide returns what a deadline change does to the event. Without an event or without a change nothing happens.
// Ask is returned for a removed deadline too: the student's «Да» then queues the removal. An unknown policy asks, so
// the event never changes without the student's say.
func Decide(policy Policy, hasEvent, dueChanged, dueCleared bool) Action {
	if !hasEvent || !dueChanged {
		return Nothing
	}

	switch policy {
	case PolicyKeep:
		return Nothing
	case PolicyUpdate:
		if dueCleared {
			return QueueRemoval
		}
		return QueueUpdate
	default:
		return Ask
	}
}
