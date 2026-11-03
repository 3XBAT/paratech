package ticketset

// Cost of a run. A ticket is one answer with citations on the strong model (Sonnet): at most answer.MaxFragments
// passages in and up to answer.MaxOutputTokens (16384) asked out, which the Hub clamps to its own cap (8192 by
// default); a Hub reask of a broken answer can double both. The figures are a typical estimate, not a bound and not a
// price list: it understates the worst case several times (passages of Russian lecture text near the input limit reach
// 30-60k input tokens, plus a full output and a reask). The Hub charges the real use against the quota; a set keeps
// the figures the student confirmed (Accepted), so a later change here never rejects a stored set.
const (
	// TicketCostMicroUSD is the typical estimate of one ticket: ~10k input tokens at $3/M and ~4k output tokens at
	// $15/M, one call without a reask.
	TicketCostMicroUSD int64 = 90_000
	// RunCapMicroUSD is the estimate above which the student must accept the cost of the run explicitly.
	RunCapMicroUSD int64 = 3_000_000
)

// Budget is the estimate of a run as the confirm card shows it.
type Budget struct {
	Tickets          int   // the tickets the run asks about
	EstimateMicroUSD int64 // the typical cost of the run
	CapMicroUSD      int64
	OverCap          bool // the student must accept the cost to confirm
}

// Estimate returns the budget of a run of tickets tickets (at most MaxTickets, so a run is bounded).
func Estimate(tickets int) Budget {
	tickets = max(0, min(tickets, MaxTickets))
	cost := int64(tickets) * TicketCostMicroUSD
	return Budget{Tickets: tickets, EstimateMicroUSD: cost, CapMicroUSD: RunCapMicroUSD, OverCap: cost > RunCapMicroUSD}
}
