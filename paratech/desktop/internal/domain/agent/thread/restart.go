package thread

import (
	"fmt"
	"time"
)

// RestartFrom is «Начать заново отсюда» → «Скрыть» (ADR-0028 «Restart from an earlier message»): requests n…last are
// dropped by a new request r, which starts as Ask starts one. Only when the current request is final: a running one
// is ErrTurnInProgress (also ErrInvalidTransition), one waiting for the student ErrInvalidTransition. Nothing is
// deleted and actions stay; the store marks the dropped requests with the new number in the save that stores the new
// request (TurnState.DropsFrom), and Window, Fill and the history read skip them. A summary that covers a dropped
// request is cleared (the next compaction rebuilds it); one of earlier requests only is kept. Requests an earlier
// restart dropped are marked again with the new number.
//
// The agent sees again what the chat shows before n: a history start an expired-request Restart set at n or later
// goes back to 0 (all), since the requests from it on are all dropped and the ones before n are kept in the history;
// a start before n stays, as it cuts only requests the restart does not touch. Lowering it to n would show nothing
// older: the requests from n on are dropped.
func (th *Thread) RestartFrom(n int, r Request, now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if t.status.stepping() {
		return fmt.Errorf("%w: %w: %s", ErrTurnInProgress, ErrInvalidTransition, t.status)
	}
	if !t.final() {
		return fmt.Errorf("%w: restart from %s", ErrInvalidTransition, t.status)
	}
	if n < 1 || n > th.turns {
		return fmt.Errorf("%w: no request %d to restart from (last %d)", ErrInvalidTransition, n, th.turns)
	}
	if err := th.Ask(r, now); err != nil {
		return err
	}
	th.current.dropsFrom = n
	if n <= th.historyFrom {
		th.historyFrom = 0
	}
	th.clearSummaryFrom(n)
	return nil
}

// DropsFrom returns the first request the current request dropped (RestartFrom); 0 when it dropped none.
func (t Turn) DropsFrom() int { return t.dropsFrom }
