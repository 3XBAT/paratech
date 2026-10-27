package thread

import (
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/agent/values"
)

// Summary returns the summary of the earlier requests 1…Through; zero when there is none. Its text is untrusted model
// output: only the summary turn of Window carries it (ADR-0028 «Context compaction»).
func (th *Thread) Summary() values.HistorySummary { return th.summary }

// SummaryAt returns when the summary was made; zero when there is none.
func (th *Thread) SummaryAt() time.Time { return th.summaryAt }

// Compact replaces the summary with one that covers more requests (CompactionPlan). It covers only requests that are
// over: below the current request, or up to it when it is final (compaction before the next Ask).
// Never while a step of a request that is not final is open. The text never enters the provenance check (seen): it is
// model output, not a tool result.
func (th *Thread) Compact(summary values.HistorySummary, now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("%w: no time", ErrInvalidThread)
	}
	if summary.IsZero() {
		return fmt.Errorf("%w: no summary", ErrInvalidCompaction)
	}
	t, err := th.turn()
	if err != nil {
		return err
	}
	if t.stepOpen && !t.final() {
		return fmt.Errorf("%w: compaction during a step", ErrInvalidTransition)
	}
	if summary.Through() <= th.summary.Through() {
		return fmt.Errorf("%w: a summary must cover more requests than the previous one", ErrInvalidCompaction)
	}
	if summary.Through() > lastOver(t) {
		return fmt.Errorf("%w: a summary covers only requests that are over", ErrInvalidCompaction)
	}
	th.summary, th.summaryAt, th.updatedAt = summary, now, now
	return nil
}

// lastOver is the number of the last request a summary may cover. A STOPPED request it covers that goes on
// («Продолжить», «Повторить») clears the summary (move).
func lastOver(t *Turn) int {
	if t.final() {
		return t.number
	}
	return t.number - 1
}

// clearSummaryFrom drops the summary when a restart from request n (RestartFrom, TASK-248) drops a request it
// covers; the next compaction rebuilds it from the kept texts (ADR-0028 «Restart from an earlier message»).
func (th *Thread) clearSummaryFrom(n int) {
	if !th.summary.IsZero() && n <= th.summary.Through() {
		th.summary, th.summaryAt = values.HistorySummary{}, time.Time{}
	}
}

// checkSummary is the Restore check of the summary: built by ParseHistorySummary, with its time, covering only
// requests that are over.
func checkSummary(s State) error {
	if s.Summary.IsZero() {
		if !s.SummaryAt.IsZero() {
			return errors.New("a summary time without a summary")
		}
		return nil
	}
	if _, err := values.ParseHistorySummary(s.Summary.Through(), s.Summary.Text()); err != nil {
		return err
	}
	if s.SummaryAt.IsZero() || s.SummaryAt.Before(s.CreatedAt) {
		return errors.New("no summary time, or before created")
	}
	if s.Current == nil || s.Summary.Through() > lastOver(&Turn{number: s.Current.Number, status: s.Current.Status}) {
		return errors.New("a summary covers a request that is not over")
	}
	return nil
}
